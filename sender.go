// Google Cloud Messaging for application servers implemented using the
// Go programming language.
package gcm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

const (
	// GcmSendEndpoint is the endpoint for sending messages to the GCM server.
	GcmSendEndpoint = "https://android.googleapis.com/gcm/send"
	// Initial delay (ms) before first retry, without jitter.
	initialBackoffDelay = 1000
	// Maximum delay (ms) before a retry.
	maxBackoffDelay = 1024000
	// Percentage jitter to use when retrying.  Jitter is a random variation in
	// timing to prevent many retries from hitting the server simulatenously.
	// Recommended values are between 0 and 50
	jitterPercentage = 50
)

// Declared as a mutable variable for testing purposes.
var gcmSendEndpoint = GcmSendEndpoint

// GCM response types
const (
	ResponseErrorMissingRegistration       = "MissingRegistration"
	ResponseErrorInvalidRegistration       = "InvalidRegistration"
	ResponseErrorMismatchSenderID          = "MismatchSenderId"
	ResponseErrorNotRegistered             = "NotRegistered"
	ResponseErrorMessageTooBig             = "MessageTooBig"
	ResponseErrorInvalidDataKey            = "InvalidDataKey"
	ResponseErrorInvalidTTL                = "InvalidTtl"
	ResponseErrorUnavailable               = "Unavailable"
	ResponseErrorInternalServerError       = "InternalServerError"
	ResponseErrorInvalidPackageName        = "InvalidPackageName"
	ResponseErrorDeviceMessageRateExceeded = "DeviceMessageRateExceeded"
)

const ()

// Sender abstracts the interaction between the application server and the
// GCM server. The developer must obtain an API key from the Google APIs
// Console page and pass it to the Sender so that it can perform authorized
// requests on the application server's behalf. To send a message to one or
// more devices use the Sender's Send or SendNoRetry methods.
//
// If the HTTP field is nil, a zeroed http.Client will be allocated and used
// to send messages. If your application server runs on Google AppEngine,
// you must use the "appengine/urlfetch" package to create the *http.Client
// as follows:
//
//	func handler(w http.ResponseWriter, r *http.Request) {
//		c := appengine.NewContext(r)
//		client := urlfetch.Client(c)
//		sender := &gcm.Sender{APIKey: key, HTTP: client}
//
//		/* ... */
//	}
type sender struct {
	APIKey string
	HTTP   *http.Client
}

func NewSender(apiKey string, client *http.Client) sender {
	return sender{
		APIKey: apiKey,
		HTTP:   client,
	}
}

// HTTPError is a custom error type to handle returning errors with GCM
// http response codes
type HTTPError struct {
	StatusCode int
	Err        error
	RetryAfter int
}

func (r *HTTPError) Error() string {
	return fmt.Sprintf("%d error: %s\nretry-after: %d", r.StatusCode, r.Err, r.RetryAfter)
}

// sendNoRetry sends a message to the GCM server without retrying in case of
// service unavailability. A non-nil error is returned if a non-recoverable
// error occurs (i.e. if the response status is not "200 OK").
//
// it should only be called from within SendWithRetry(), and so all the checks
// are assumed to have already been done.
func (s *sender) sendNoRetry(encodedMsg []byte) (*Response, *HTTPError) {

	// set up the GCM request
	req, err := http.NewRequest("POST", gcmSendEndpoint, bytes.NewBuffer(encodedMsg))
	if err != nil {
		return nil, &HTTPError{Err: err}
	}
	req.Header.Add("Authorization", fmt.Sprintf("key=%s", s.APIKey))
	req.Header.Add("Content-Type", "application/json")

	resp, err := s.HTTP.Do(req)
	defer resp.Body.Close()
	if err != nil {
		return nil, &HTTPError{Err: err}
	}

	// if the status is not StatusOK, return with the error
	if resp.StatusCode != http.StatusOK {
		delta, err := parseRetryAfter(resp.Header.Get("Retry-After"))
		if err != nil {
			return nil, &HTTPError{
				StatusCode: resp.StatusCode,
				Err:        errors.New(resp.Status),
			}
		}
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Err:        errors.New(resp.Status),
			RetryAfter: delta,
		}
	}

	// get the body and decode
	response := new(Response)
	err = json.NewDecoder(resp.Body).Decode(response)
	if err != nil {
		return response, &HTTPError{Err: err}
	}
	// success response
	return response, &HTTPError{StatusCode: http.StatusOK}
}

// Send sends a message to the GCM server, retrying in case of service
// unavailability. A non-nil error is returned if a non-recoverable
// error occurs (i.e. if the response status is not "200 OK").
//
// Note that messages are retried using exponential backoff, and as a
// result, this method may block for several seconds.
func (s *sender) Send(msg *Message, gcmIDs []string, retries int) (*Response, *HTTPError) {
	// There must be at least one retry
	if retries < 0 {
		return nil, &HTTPError{Err: fmt.Errorf("'retries' must be positive")}
	}

	if err := s.validate(); err != nil {
		return nil, &HTTPError{Err: err}
	} else if err := msg.validate(); err != nil {
		return nil, &HTTPError{Err: err}
	}

	jsonMsg, err := json.Marshal(msg)
	if err != nil {
		return nil, &HTTPError{Err: err}
	}

	// Send the message for the first time.
	resp, httpErr := s.sendNoRetry(jsonMsg)
	if httpErr.Err != nil {
		return nil, httpErr
	}

	// if there were no errors, or retries = 0, return the result
	if resp.Failure == 0 || retries == 0 {
		return resp, httpErr // err should always be nil here
	}

	// One or more messages failed to send.
	regIDs := msg.RegistrationIDs // store the original RegistrationIDs
	// TODO: Don't modify the msg object via the pointer, as we may have
	// multiple goroutines acting on it at once
	allResults := make(map[string]Result, len(regIDs))
	backoff := initialBackoffDelay
	for i := 0; updateStatus(msg, resp, allResults) > 0 && i < retries; i++ {
		sleepTime := calculateSleep(backoff)
		time.Sleep(time.Duration(sleepTime) * time.Millisecond)
		backoff = min(2*backoff, maxBackoffDelay)
		if resp, httpErr = s.sendNoRetry(jsonMsg); httpErr.Err != nil {
			// set registration ids back to their original values
			msg.RegistrationIDs = regIDs
			return nil, httpErr
		}
	}

	// Bring the message back to its original state.
	msg.RegistrationIDs = regIDs

	// Create a Response containing the overall results.
	finalResults := make([]Result, len(regIDs))
	var success, failure, canonicalIDs int
	for i := 0; i < len(regIDs); i++ {
		result, _ := allResults[regIDs[i]]
		finalResults[i] = result
		if result.MessageID != "" {
			if result.RegistrationID != "" {
				canonicalIDs++
			}
			success++
		} else {
			failure++
		}
	}

	return &Response{
		// Return the most recent multicast id.
		MulticastID:  resp.MulticastID,
		Success:      success,
		Failure:      failure,
		CanonicalIDs: canonicalIDs,
		Results:      finalResults,
	}, httpErr
}

// parseRetryAfter attempts to parse the contents of the Retry-After http
// header.  It returns the time in seconds if successful, and an error for
// either an unparseable value or a nil value.
func parseRetryAfter(r string) (int, error) {
	// if not set
	if r == "" {
		return 0, fmt.Errorf("Empty Retry-After header")
	}

	// if set as an integer delta (assumed to be in seconds)
	if delta, err := strconv.Atoi(r); err == nil {
		return max(delta, 0), err
	}

	// if set as a date, convert to a time in seconds
	if ra, err := time.Parse(time.RFC1123, r); err == nil {
		delta := (ra.UnixNano() - time.Now().UnixNano()) / 1e9
		return int(delta), err
	}
	fmt.Printf("#936r Unparseable 'Retry-After' header: %s", r)
	return 0, fmt.Errorf("Unparseable 'Retry-After' header: %s", r)
}

func calculateSleep(backoff int) int {
	return (backoff*(100-jitterPercentage))/100 + rand.Intn((2*backoff*jitterPercentage)/100)
}

// updateStatus updates the status of the messages sent to devices and
// returns the number of recoverable errors that could be retried.
func updateStatus(msg *Message, resp *Response, allResults map[string]Result) int {
	unsentRegIDs := make([]string, 0, resp.Failure)
	for i := 0; i < len(resp.Results); i++ {
		regID := msg.RegistrationIDs[i]
		allResults[regID] = resp.Results[i]
		if resp.Results[i].Error == "Unavailable" {
			unsentRegIDs = append(unsentRegIDs, regID)
		}
	}
	msg.RegistrationIDs = unsentRegIDs
	return len(unsentRegIDs)
}

// min returns the smaller of two integers. For exciting religious wars
// about why this wasn't included in the "math" package, see this thread:
// https://groups.google.com/d/topic/golang-nuts/dbyqx_LGUxM/discussion
func min(a, b int) int {
	if a <= b {
		return a
	}
	return b
}

// max returns the larger of two integers.
func max(a, b int) int {
	if a >= b {
		return a
	}
	return b
}

// validate returns an error if the sender is not well-formed and
// initializes a zeroed http.Client if one has not been provided.
func (s *sender) validate() error {
	if s.APIKey == "" {
		return errors.New("the sender's API key must not be empty")
	}
	if s.HTTP == nil {
		return errors.New("an http client must be provided")
	}
	return nil
}

// validate returns an error if the message is not well-formed.
func (msg *Message) validate() error {
	if msg == nil {
		return errors.New("the message must not be nil")
	} else if msg.RegistrationIDs == nil {
		return errors.New("the message's RegistrationIDs field must not be nil")
	} else if len(msg.RegistrationIDs) == 0 {
		return errors.New("the message must specify at least one registration ID")
	} else if len(msg.RegistrationIDs) > 1000 {
		return errors.New("the message may specify at most 1000 registration IDs")
	} else if msg.TimeToLive < 0 || 2419200 < msg.TimeToLive {
		return errors.New("the message's TimeToLive field must be an integer " +
			"between 0 and 2419200 (4 weeks)")
	}
	return nil
}
