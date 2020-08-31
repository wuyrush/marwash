package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestWasher(t *testing.T) {
	tcs := []struct {
		name     string
		wmock    *walkerMock
		pmock    *pingerMock
		expTuple []Result
	}{
		{
			name: "HappyCase",
			wmock: func() *walkerMock {
				m := &walkerMock{}
				m.On("Next").Return(&Bookmark{URL: "https://foo"}, nil).Once()
				m.On("Next").Return(&Bookmark{URL: "https://bar"}, nil).Once()
				m.On("Next").Return(&Bookmark{URL: "https://qux"}, nil).Once()
				m.On("Next").Return((*Bookmark)(nil), io.EOF).Once()
				return m
			}(),
			pmock: func() *pingerMock {
				m := &pingerMock{}
				m.On("Ping", "https://foo").Return(Alive, nil)
				m.On("Ping", "https://bar").Return(Dead, nil)
				m.On("Ping", "https://qux").Return(Unknown, nil)
				return m
			}(),
			expTuple: []Result{
				{&Bookmark{URL: "https://foo", Status: Alive}, nil},
				{&Bookmark{URL: "https://bar", Status: Dead}, nil},
				{&Bookmark{URL: "https://qux", Status: Unknown}, nil},
			},
		},
		{
			name: "Empty",
			wmock: func() *walkerMock {
				m := &walkerMock{}
				m.On("Next").Return((*Bookmark)(nil), io.EOF).Once()
				return m
			}(),
			pmock:    &pingerMock{},
			expTuple: []Result{},
		},
		{
			name: "WalkerError",
			wmock: func() *walkerMock {
				m := &walkerMock{}
				m.On("Next").Return(&Bookmark{URL: "https://foo"}, nil).Once()
				m.On("Next").Return((*Bookmark)(nil), errors.New("boom!")).Once()
				return m
			}(),
			pmock: func() *pingerMock {
				m := &pingerMock{}
				m.On("Ping", "https://foo").Return(Alive, nil)
				return m
			}(),
			expTuple: []Result{
				{&Bookmark{URL: "https://foo", Status: Alive}, nil},
				{nil, errors.New("boom!")},
			},
		},
		{
			name: "PingerError",
			wmock: func() *walkerMock {
				m := &walkerMock{}
				m.On("Next").Return(&Bookmark{URL: "https://foo"}, nil).Once()
				m.On("Next").Return(&Bookmark{URL: "https://bar"}, nil).Once()
				m.On("Next").Return(&Bookmark{URL: "https://qux"}, nil).Once()
				m.On("Next").Return((*Bookmark)(nil), io.EOF).Once()
				return m
			}(),
			pmock: func() *pingerMock {
				m := &pingerMock{}
				m.On("Ping", "https://foo").Return(Alive, nil)
				m.On("Ping", "https://bar").Return(Unknown, errors.New("poom!"))
				m.On("Ping", "https://qux").Return(Dead, nil)
				return m
			}(),
			expTuple: []Result{
				{&Bookmark{URL: "https://foo", Status: Alive}, nil},
				{&Bookmark{URL: "https://bar", Status: Unknown}, errors.New("poom!")},
				{&Bookmark{URL: "https://qux", Status: Dead}, nil},
			},
		},
	}
	log := genTstLogger()
	for _, c := range tcs {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			washer := NewWasher(c.wmock, c.pmock, log, 2)
			defer washer.Stop()
			actual := []Result{}
			for {
				b, err := washer.Next()
				if err == io.EOF {
					break
				}
				actual = append(actual, Result{b, err})
			}
			c.wmock.AssertExpectations(t)
			c.pmock.AssertExpectations(t)
			assert.Equal(t, len(c.expTuple), len(actual))
			// concurrency in washer makes ordering unpredictable
			for _, tp := range c.expTuple {
				assert.Contains(t, actual, tp)
			}
			_, err := washer.Next()
			assert.Equal(t, err, io.EOF, "calling Next() after exhausing the washer should have return io.EOF")
		})
	}
}

func TestWasherStopEarly(t *testing.T) {
	// infinite bookmarks to wash
	wmock := &walkerMock{}
	wmock.On("Next").Return(&Bookmark{URL: fmt.Sprintf("https://foo%d", rand.Intn(1024))}, nil)
	pmock := &pingerMock{}
	pmock.On("Ping", mock.Anything).Return(Alive, nil)
	log := genTstLogger()
	washer := NewWasher(wmock, pmock, log, 2)
	// start iterating washer
	started, start, done := false, make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			b, err := washer.Next()
			if !started {
				started = true
				close(start)
			}
			if err != nil {
				assert.Empty(t, b, "no bookmark data should have present on exhausting the washer")
				assert.Equalf(t, io.EOF, err, "expect io.EOF on exhuasting washer but got %v", err)
				return
			}
			assert.NotEmpty(t, b, "bookmark data should have present when washer is not exhausted")
		}
	}()
	// stop washer after iteration on washer had started
	<-start
	washer.Stop()
	timeout := time.NewTimer(1 * time.Second)
	select {
	case <-done:
	case <-timeout.C:
		t.Error("washer is not stopped even after timeout elapsed, indicating buggy washer stop mechanism")
	}
}

func TestStartWashTillDone(t *testing.T) {
	tcs := []struct {
		name      string
		in        io.Reader
		out       *bytes.Buffer
		verifyOut func(t *testing.T, out *bytes.Buffer)
		dmock     *doerMock
	}{
		{
			name: "EmptyInput",
			in:   bytes.NewReader([]byte{}),
			out:  bytes.NewBuffer(make([]byte, 0)),
			verifyOut: func(t *testing.T, out *bytes.Buffer) {
				b, err := ioutil.ReadAll(out)
				assert.Nil(t, err, "readall from empty buffer should have succeeded")
				assert.Emptyf(t, b, "expect empty output but got %s", b)
			},
			dmock: &doerMock{},
		},
		{
			name: "HappyPath",
			in: bytes.NewReader([]byte(`<!-- This is an automatically generated file.
     It will be read and overwritten.
     DO NOT EDIT! -->
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks</TITLE>
<H1>Bookmarks</H1>
<DL><p>
    <DT><H3 ADD_DATE="1512790922" LAST_MODIFIED="1588537285" PERSONAL_TOOLBAR_FOLDER="true">FooDir</H3>
    <DL><p>
		<DT><A HREF="https://bar.io" ADD_DATE="1515361177" ICON="data:image/png;base64,blah==">Bar</A>
        <DT><H3 ADD_DATE="1515126229" LAST_MODIFIED="1592885828">BarDir</H3>
        <DL><p>
			<DT><A HREF="https://qux.io" ADD_DATE="1515361177" ICON="data:image/png;base64,blah==">Qux</A>
            <DT><H3 ADD_DATE="1516481807" LAST_MODIFIED="1573835459">Qux Dir</H3>
            <DL><p>
				<DT><A HREF="https://bee.io" ADD_DATE="1515361173" ICON="data:image/png;base64,blahblah==">Bee</A>
				<DT><A HREF="https://foo.io" ADD_DATE="1515361173" ICON="data:image/png;base64,blahblah==">Foo</A>
`)),
			dmock: func() *doerMock {
				m := &doerMock{}
				m.On("Do", mock.Anything).Return(genResp(http.StatusGone), nil)
				return m
			}(),
			out: bytes.NewBuffer(make([]byte, 0)),
			verifyOut: func(t *testing.T, out *bytes.Buffer) {
				b, err := ioutil.ReadAll(out)
				assert.Nil(t, err, "readall from empty buffer should have succeeded")
				// so that it is easier to debug than peeking raw bytes
				output := string(b)
				for _, name := range []string{"bar", "qux", "bee", "foo"} {
					rec := fmt.Sprintf("dead\thttps://%s.io\tGone\n", name)
					assert.Contains(t, output, rec)
				}
			},
		},
	}
	log := genTstLogger()
	cquota := 14
	for _, c := range tcs {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			StartWashTillDone(c.in, c.out, c.dmock, cquota, log)
			c.dmock.AssertExpectations(t)
			c.verifyOut(t, c.out)
		})
	}
}

func TestStartWashTillDoneStopOnSignal(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		in := &infReader{}
		out := ioutil.Discard
		dmock := &doerMock{}
		washstart, started := make(chan struct{}), false
		dmock.On("Do", mock.Anything).Run(func(args mock.Arguments) {
			if !started {
				started = true
				close(washstart)
			}
		}).Return(genResp(http.StatusOK), nil)
		log := genTstLogger()
		cquota := 14

		aborted := make(chan struct{})
		go func() {
			defer close(aborted)
			StartWashTillDone(in, out, dmock, cquota, log)
		}()
		<-washstart
		timeout := time.NewTimer(500 * time.Millisecond)
		defer timeout.Stop()
		// send OS signal to this process. Inspired by UTs against os/signal: https://golang.org/src/os/signal/signal_test.go
		t.Logf("%s...", sig.String())
		syscall.Kill(syscall.Getpid(), sig)
		select {
		case <-aborted:
		case <-timeout.C:
			t.Error("StartWashTillDone should have aborted after receiving OS signal")
		}
	}
}

type walkerMock struct {
	Walker
	mock.Mock
}

func (m *walkerMock) Next() (*Bookmark, error) {
	args := m.Called()
	return args.Get(0).(*Bookmark), args.Error(1)
}

func (m *walkerMock) Stop() {
	m.Called()
}

type pingerMock struct {
	Pinger
	mock.Mock
}

func (m *pingerMock) Ping(url string) (PingStatus, error) {
	args := m.Called(url)
	return args.Get(0).(PingStatus), args.Error(1)
}
