package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"

	urlpkg "net/url"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestPinger_alive(t *testing.T) {

	url := "https://foo.bar"
	tcs := []struct {
		name   string
		dmock  *doerMock
		expErr error
	}{
		{
			name: "HappyPath",
			dmock: func() *doerMock {
				m := &doerMock{}
				m.On("Do", mock.Anything).Run(func(args mock.Arguments) {
					reqAsExpected(t, args.Get(0).(*http.Request), url, http.MethodHead)
				}).Return(genResp(http.StatusOK), nil)
				return m
			}(),
		},
		{
			name: "RetriedOnRetryableError",
			dmock: func() *doerMock {
				m := &doerMock{}
				m.On("Do", mock.Anything).Run(func(args mock.Arguments) {
					reqAsExpected(t, args.Get(0).(*http.Request), url, http.MethodHead)
				}).Return((*http.Response)(nil), &urlpkg.Error{Err: errMock{Tmout: true}}).Once()
				m.On("Do", mock.Anything).Return((*http.Response)(nil), &urlpkg.Error{Err: errMock{Temp: true}}).Once()
				m.On("Do", mock.Anything).
					Return(genResp(http.StatusOK), nil).Once()
				return m
			}(),
		},
		{
			name: "RetriedOnRetryableStatus",
			dmock: func() *doerMock {
				m := &doerMock{}
				m.On("Do", mock.Anything).Run(func(args mock.Arguments) {
					reqAsExpected(t, args.Get(0).(*http.Request), url, http.MethodHead)
				}).Return(genResp(http.StatusInternalServerError), nil).Once()
				m.On("Do", mock.Anything).Return(genResp(http.StatusServiceUnavailable), nil).Once()
				m.On("Do", mock.Anything).Return(genResp(http.StatusOK), nil).Once()
				return m
			}(),
		},
		{
			name: "RetriedOnDifferentPingStrategies",
			dmock: func() *doerMock {
				m := &doerMock{}
				m.On("Do", mock.Anything).Run(func(args mock.Arguments) {
					reqAsExpected(t, args.Get(0).(*http.Request), url, http.MethodHead)
				}).Return((*http.Response)(nil), &urlpkg.Error{Err: errMock{Tmout: true}}).Once()
				m.On("Do", mock.Anything).Return((*http.Response)(nil), &urlpkg.Error{Err: errMock{Temp: true}}).Once()
				m.On("Do", mock.Anything).Return(genResp(http.StatusInternalServerError), nil).Once()
				m.On("Do", mock.Anything).Run(func(args mock.Arguments) {
					reqAsExpected(t, args.Get(0).(*http.Request), url, http.MethodGet)
				}).Return(genResp(http.StatusServiceUnavailable), nil).Once()
				m.On("Do", mock.Anything).Return(genResp(http.StatusOK), nil).Once()
				return m
			}(),
		},
	}
	log := genTstLogger()
	for _, cs := range tcs {
		// capture the current iteration variable value(aka the value of cs variable in current iteration) otherwise
		// there is a race between iteration variable value changing over iteration and multiple goroutines running
		// sub-tests accessing the same iteration variable
		c := cs
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			pinger := NewPinger(c.dmock, log)
			status, err := pinger.Ping(url)
			c.dmock.AssertExpectations(t)
			assert.Equal(t, Alive, status)
			assert.Equal(t, c.expErr, err)
		})
	}
}

func TestPinger_dead(t *testing.T) {
	url := "https://dead.url"
	type tcase struct {
		name   string
		dmock  *doerMock
		expErr error
	}
	genCase := func(code int) tcase {
		return tcase{
			name: http.StatusText(code),
			dmock: func() *doerMock {
				m := &doerMock{}
				m.On("Do", mock.Anything).Run(func(args mock.Arguments) {
					reqAsExpected(t, args.Get(0).(*http.Request), url, http.MethodHead)
				}).Return(genResp(code), nil).Once()
				return m
			}(),
			expErr: statusNotAlive(code),
		}
	}
	tcs := []tcase{
		genCase(http.StatusGone),
		genCase(http.StatusConflict),
		genCase(http.StatusRequestEntityTooLarge),
		genCase(http.StatusRequestURITooLong),
		genCase(http.StatusUnprocessableEntity),
		genCase(http.StatusFailedDependency),
		genCase(http.StatusNotImplemented),
	}
	log := genTstLogger()
	for _, cs := range tcs {
		c := cs
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			pinger := NewPinger(c.dmock, log)
			status, err := pinger.Ping(url)
			c.dmock.AssertExpectations(t)
			assert.Equal(t, Dead, status)
			assert.Equal(t, c.expErr, err)
		})
	}
}

func TestPinger_dead_BadURL(t *testing.T) {
	badUrls := []string{
		"http://foo\n",
	}
	log := genTstLogger()
	for _, url := range badUrls {
		t.Run(url, func(t *testing.T) {
			pinger := NewPinger(nil, log)
			status, err := pinger.Ping(url)
			assert.Equal(t, Dead, status)
			assert.NotEmpty(t, err)
		})
	}
}

func TestPinger_unknown(t *testing.T) {
	// TODO Table-driven test: Get the 1st case running and then all others will follow
	url := "https://unknown.url"
	type tcase struct {
		name   string
		dmock  *doerMock
		expErr error
	}
	genCase := func(name string, code int, doErr error, headTries, getTries int) tcase {
		return tcase{
			name: name,
			dmock: func() *doerMock {
				m := &doerMock{}
				m.On("Do", mock.Anything).Run(func(args mock.Arguments) {
					reqAsExpected(t, args.Get(0).(*http.Request), url, http.MethodHead)
				}).Return(genResp(code), doErr).Times(headTries)
				m.On("Do", mock.Anything).Run(func(args mock.Arguments) {
					reqAsExpected(t, args.Get(0).(*http.Request), url, http.MethodGet)
				}).Return(genResp(code), doErr).Times(getTries)
				return m
			}(),
			expErr: doErr,
		}
	}
	tcs := []tcase{
		genCase("NetErrNonRetryable", 0, &urlpkg.Error{Err: errMock{Tmout: false, Temp: false}}, 1, 0),
		genCase("NetErrTimeout", 0, &urlpkg.Error{Err: errMock{Tmout: true}}, 3, 3),
		genCase("NetErrTemp", 0, &urlpkg.Error{Err: errMock{Temp: true}}, 3, 3),
	}
	// add cases where we exhausted retries on retryable status code
	for code, tries := range map[int][]int{
		http.StatusNotFound:            {1, 0},
		http.StatusMisdirectedRequest:  {3, 3},
		http.StatusTooManyRequests:     {3, 3},
		http.StatusInternalServerError: {3, 3},
		http.StatusBadGateway:          {3, 3},
		http.StatusServiceUnavailable:  {3, 3},
		http.StatusGatewayTimeout:      {3, 3},
		http.StatusInsufficientStorage: {3, 3},
	} {
		tcs = append(tcs, genCase(http.StatusText(code), code, statusNotAlive(code), tries[0], tries[1]))
	}
	log := genTstLogger()
	for _, cs := range tcs {
		c := cs
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			pinger := NewPinger(c.dmock, log)
			status, err := pinger.Ping(url)
			c.dmock.AssertExpectations(t)
			assert.Equal(t, Unknown, status)
			assert.Equal(t, c.expErr, err)
		})
	}
}

func reqAsExpected(t *testing.T, req *http.Request, url, expMethod string) {
	assert.Equal(t, url, req.URL.String())
	assert.Equal(t, expMethod, req.Method)
	assert.NotEmpty(t, req.Header.Get("User-Agent"))
}

func genResp(code int) *http.Response {
	return &http.Response{StatusCode: code, Body: ioutil.NopCloser(bytes.NewReader([]byte{}))}
}

type doerMock struct {
	Doer
	mock.Mock
}

func (m *doerMock) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

type errMock struct {
	Tmout, Temp bool
}

func (e errMock) Timeout() bool {
	return e.Tmout
}

func (e errMock) Temporary() bool {
	return e.Temp
}

func (e errMock) Error() string {
	return fmt.Sprintf("%#v", e)
}

func genTstLogger() *zap.SugaredLogger {
	lg, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	return lg.Sugar()
}
