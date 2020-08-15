package main

import (
	"io"
	"testing"

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
				{Bookmark{URL: "https://foo", Status: Alive}, nil},
				{Bookmark{URL: "https://bar", Status: Dead}, nil},
				{Bookmark{URL: "https://qux", Status: Unknown}, nil},
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
				actual = append(actual, bmkErrTuple{*b, err})
			}
			c.wmock.AssertExpectations(t)
			c.pmock.AssertExpectations(t)
			// concurrency in washer makes ordering unpredictable
			for _, tp := range c.expTuple {
				assert.Contains(t, actual, tp)
			}
			_, err := washer.Next()
			assert.Equal(t, err, io.EOF, "calling Next() after exhausing the washer should have return io.EOF")
		})
	}
}

// choose not to depend on unexported types used in source package as they may become inaccessible to the UTs in
// the future. This prevents UTs from becoming brittle in refactor.
type bmkErrTuple struct {
	B Bookmark
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
