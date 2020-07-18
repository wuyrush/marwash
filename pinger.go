package main

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	urlpkg "net/url"
	"time"

	"github.com/avast/retry-go"
)

// Pinger checks whether a given URL is reachable or not.
// Pinger should be safe for concurrent use.
type Pinger struct {
	HTTPClient *http.Client
	pingFns    []pingFn
}

// pingFn pings url and return ping status and error encountered.
type pingFn func(url string) (PingStatus, error)

// NewPinger returns a new Pinger.
func NewPinger(hc *http.Client) *Pinger {
	p := &Pinger{HTTPClient: hc}
	p.pingFns = []pingFn{
		func(url string) (PingStatus, error) { return p.ping(url, http.MethodHead) },
		func(url string) (PingStatus, error) { return p.ping(url, http.MethodGet) },
	}
	return p
}

// Ping pings url to determine whether it is reachable or not.
func (p *Pinger) Ping(url string) (status PingStatus, err error) {
	for _, f := range p.pingFns {
		status, err = f(url)
		if terminal(status, err) {
			return
		}
	}
	return
}

// terminal returns true if the given ping result necessitates no retries.
func terminal(status PingStatus, err error) bool {
	return status == Alive || status == Dead || noRetry(err)
}

// noRetry returns true if err necessitates no retries.
func noRetry(err error) bool {
	// TODO: implement
	return true
}

func (p *Pinger) ping(url, method string) (PingStatus, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return Unknown, err
	}
	var resp *http.Response
	err = retry.Do(
		func() error {
			resp, err = p.HTTPClient.Do(req)
			if err == nil && !alive(resp.StatusCode) {
				// read up response body and close it so that connection can be reused for next retry if any
				blackhole(resp.Body)
				err = statusNotAlive(resp.StatusCode)
			}
			return err
		},
		retry.RetryIf(errOrStatusRetryable),
		retry.LastErrorOnly(true),
		retry.Attempts(3),
		retry.Delay(100*time.Millisecond),   // default delay
		retry.DelayType(retry.BackOffDelay), // exponential backoff
	)
	if _, ok := err.(statusNotAlive); err != nil && !ok {
		return Unknown, err
	}
	// no point to read up body as bookmarks are usually unique to each other, plus we've done all retries
	_ = resp.Body.Close()
	return check(resp.StatusCode), nil
}

func errOrStatusRetryable(err error) bool {
	switch v := err.(type) {
	case *urlpkg.Error:
		return v.Temporary() || v.Timeout()
	case statusNotAlive:
		_, ok := retryCodes[int(v)]
		return ok
	default:
		// this should never happen
		panic(fmt.Sprintf("ping: got error of unknown type %T", err))
	}
}

func blackhole(respBody io.ReadCloser) {
	defer respBody.Close()
	br := bufio.NewReader(respBody)
	_, _ = br.WriteTo(ioutil.Discard)
}

type statusNotAlive int

func (s statusNotAlive) Error() string {
	return http.StatusText(int(s))
}

func check(code int) PingStatus {
	if _, ok := deadCodes[code]; ok {
		return Dead
	} else if alive(code) {
		return Alive
	}
	// unknown = all - alive - dead
	return Unknown
}

type PingStatus int

const (
	Unknown PingStatus = iota
	Alive
	Dead
)

var pingStatuses = []string{"unknown", "alive", "dead"}

func (s PingStatus) String() string {
	return pingStatuses[int(s)]
}

// alive returns true if respon status code indicates a reachable URL
func alive(code int) bool {
	return code < 300 && code >= 200
}

// set of status codes which we consider the URL is hard dead.
var deadCodes = map[int]struct{}{
	http.StatusConflict:              {}, // most likely associated with PUT request instead of HEAD and GET
	http.StatusGone:                  {}, // server intentionally knows the resource is unavailable
	http.StatusRequestEntityTooLarge: {}, // HEAD/GET has no request body
	http.StatusRequestURITooLong:     {}, // nearly impossible for user with browser to encounter such problem
	http.StatusUnprocessableEntity:   {}, // HEAD/GET has no request body
	http.StatusFailedDependency:      {}, // no dep for HEAD/GET
	http.StatusNotImplemented:        {},
}

var retryCodes = map[int]struct{}{
	http.StatusMisdirectedRequest:  {}, // MAY retry with a new connection
	http.StatusTooManyRequests:     {}, // can retry with the same conn
	http.StatusInternalServerError: {},
	http.StatusBadGateway:          {},
	http.StatusServiceUnavailable:  {},
	http.StatusGatewayTimeout:      {},
	http.StatusInsufficientStorage: {},
	599:                            {},
}
