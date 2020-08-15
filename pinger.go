package main

import (
	"bufio"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	urlpkg "net/url"
	"time"

	"github.com/avast/retry-go"
	"go.uber.org/zap"
)

// Pinger checks whether a given URL is reachable or not.
// Pinger should be safe for concurrent use.
type Pinger interface {
	Ping(url string) (status PingStatus, err error)
}

// HTTPinger checks URLs in HTTP/S scheme.
type HTTPinger struct {
	Doer    Doer
	Log     *zap.SugaredLogger
	pingFns []pingFn
}

// Doer is an abstraction over *http.Client.Do in std lib. It is to achieve better testability than
// using the plain http.Client struct.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// pingFn pings url and return ping status and error encountered.
type pingFn func(url string) (PingStatus, error)

// NewHTTPinger returns a new HTTPinger.
func NewHTTPinger(doer Doer, log *zap.SugaredLogger) *HTTPinger {
	p := &HTTPinger{Doer: doer, Log: log}
	p.pingFns = []pingFn{
		func(url string) (PingStatus, error) { return p.ping(url, http.MethodHead) },
		func(url string) (PingStatus, error) { return p.ping(url, http.MethodGet) },
	}
	return p
}

// Ping pings url to determine whether it is reachable or not.
func (p *HTTPinger) Ping(url string) (status PingStatus, err error) {
	for _, f := range p.pingFns {
		status, err = f(url)
		if terminal(status, err) {
			return
		}
	}
	return
}

// terminal tells if we need to continue pinging(with a different strategy) based on ping result
func terminal(status PingStatus, err error) bool {
	if status == Alive || status == Dead {
		// reachability already known
		return true
	} else if v, ok := err.(statusNotAlive); ok && http.StatusMethodNotAllowed == int(v) {
		// we can continue pinging with a different http method
		return false
	}
	return !errOrStatusRetryable(err)
}

func (p *HTTPinger) ping(url, method string) (PingStatus, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return Dead, err
	}
	req.Header.Add("User-Agent", randUserAgent())
	var resp *http.Response
	err = retry.Do(
		func() error {
			resp, err = p.Doer.Do(req)
			if err == nil && !alive(resp.StatusCode) {
				// make sure connection can be reused for successive retries, if any
				p.blackhole(resp.Body)
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
	// make error, be it due to network / bad response,  accessible to users
	return check(resp.StatusCode), err
}

func errOrStatusRetryable(err error) bool {
	switch v := err.(type) {
	case *urlpkg.Error:
		return v.Temporary() || v.Timeout()
	case statusNotAlive:
		_, ok := retryable[int(v)]
		return ok
	default:
		return false
	}
}

// blackhole reads and discards data from rc to EOF
func (p *HTTPinger) blackhole(rc io.ReadCloser) {
	defer rc.Close()
	br := bufio.NewReader(rc)
	_, err := br.WriteTo(ioutil.Discard)
	if err != nil {
		p.Log.Errorw("failed to discard input data", "error", err)
	}
}

type statusNotAlive int

func (s statusNotAlive) Error() string {
	return http.StatusText(int(s))
}

// check return ping status based on ping response status code.
func check(code int) PingStatus {
	if _, ok := dead[code]; ok {
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

// set of status codes which we consider the URL is dead.
var dead = map[int]struct{}{
	http.StatusConflict:              {}, // most likely associated with PUT request instead of HEAD and GET
	http.StatusGone:                  {}, // server intentionally knows the resource is unavailable
	http.StatusRequestEntityTooLarge: {}, // HEAD/GET has no request body
	http.StatusRequestURITooLong:     {}, // nearly impossible for user with browser to encounter such problem
	http.StatusUnprocessableEntity:   {}, // HEAD/GET has no request body
	http.StatusFailedDependency:      {}, // no dep for HEAD/GET
	http.StatusNotImplemented:        {},
}

var retryable = map[int]struct{}{
	http.StatusMisdirectedRequest:  {}, // MAY retry with a new connection
	http.StatusTooManyRequests:     {}, // can retry with the same conn
	http.StatusInternalServerError: {},
	http.StatusBadGateway:          {},
	http.StatusServiceUnavailable:  {},
	http.StatusGatewayTimeout:      {},
	http.StatusInsufficientStorage: {},
	599:                            {},
}

func randUserAgent() string {
	return userAgentHeaders[rand.Int()%len(userAgentHeaders)]
}

var userAgentHeaders = []string{
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:78.0) Gecko/20100101 Firefox/78.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.14; rv:76.0) Gecko/20100101 Firefox/76.0",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/51.0.2704.103 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/51.0.2704.106 Safari/537.36 OPR/38.0.2220.41",
	"Mozilla/5.0 (compatible; MSIE 9.0; Windows Phone OS 7.5; Trident/5.0; IEMobile/9.0)",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 13_5_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.1.1 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.1.2 Safari/605.1.15",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.36 Edge/16.16299",
	"Mozilla/5.0 (Linux; Android 9.0.4; Google Pixel) AppleWebKit/535.19 (KHTML, like Gecko) Chrome/18.0.1025.133 Mobile Safari/535.19",
}
