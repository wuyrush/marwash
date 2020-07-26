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
	userAgentStr := "User-Agent"
	url := "https://foo.bar"
	reqAsExpected := func(req *http.Request, expMethod string) {
		assert.Equal(t, url, req.URL.String())
		assert.Equal(t, expMethod, req.Method)
		assert.NotEmpty(t, req.Header.Get(userAgentStr))
	}
	genResp := func(code int) *http.Response {
		return &http.Response{StatusCode: code, Body: ioutil.NopCloser(bytes.NewReader([]byte{}))}
	}
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
					reqAsExpected(args.Get(0).(*http.Request), http.MethodHead)
				}).Return(genResp(http.StatusOK), nil)
				return m
			}(),
		},
		{
			name: "RetriedOnRetryableError",
			dmock: func() *doerMock {
				m := &doerMock{}
				m.On("Do", mock.Anything).Run(func(args mock.Arguments) {
					reqAsExpected(args.Get(0).(*http.Request), http.MethodHead)
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
					reqAsExpected(args.Get(0).(*http.Request), http.MethodHead)
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
					reqAsExpected(args.Get(0).(*http.Request), http.MethodHead)
				}).Return((*http.Response)(nil), &urlpkg.Error{Err: errMock{Tmout: true}}).Once()
				m.On("Do", mock.Anything).Return((*http.Response)(nil), &urlpkg.Error{Err: errMock{Temp: true}}).Once()
				m.On("Do", mock.Anything).Return(genResp(http.StatusInternalServerError), nil).Once()
				m.On("Do", mock.Anything).Run(func(args mock.Arguments) {
					reqAsExpected(args.Get(0).(*http.Request), http.MethodGet)
				}).Return(genResp(http.StatusServiceUnavailable), nil).Once()
				m.On("Do", mock.Anything).Return(genResp(http.StatusOK), nil).Once()
				return m
			}(),
		},
	}
	log := genTstLogger()
	for _, c := range tcs {
		t.Run(c.name, func(t *testing.T) {
			pinger := NewPinger(c.dmock, log)
			status, err := pinger.Ping(url)
			c.dmock.AssertExpectations(t)
			assert.Equal(t, Alive, status)
			assert.Equal(t, error(nil), err)
		})
	}
}

// TODO: implement
func TestPinger_dead(t *testing.T) {
	t.Fail()
}

// TODO: implement
func TestPinger_unknown(t *testing.T) {
	t.Fail()
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
