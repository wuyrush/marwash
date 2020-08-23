package main

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
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
		expTuple []bmkErrTuple
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
			expTuple: []bmkErrTuple{
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
			expTuple: []bmkErrTuple{},
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
			expTuple: []bmkErrTuple{
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
			expTuple: []bmkErrTuple{
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
			actual := []bmkErrTuple{}
			for {
				b, err := washer.Next()
				if err == io.EOF {
					break
				}
				actual = append(actual, bmkErrTuple{b, err})
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

// choose not to depend on unexported types used in source package as they may become inaccessible to the UTs in
// the future. This prevents UTs from becoming brittle in refactor.
type bmkErrTuple struct {
	B *Bookmark
	E error
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
