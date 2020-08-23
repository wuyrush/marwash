package main

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNetscapeWalker(t *testing.T) {
	tcs := []struct {
		name         string
		reader       io.Reader
		expBookmarks []*Bookmark
		expErrs      []bool // expect a non-nil error or not
	}{
		{
			name: "HappyCase",
			reader: bytes.NewReader([]byte(`<!DOCTYPE NETSCAPE-Bookmark-file-1>
<!-- This is an automatically generated file.
     It will be read and overwritten.
     DO NOT EDIT! -->
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks</TITLE>
<H1>Bookmarks</H1>
<DL><p>
    <DT><H3 ADD_DATE="1512790922" LAST_MODIFIED="1588537285" PERSONAL_TOOLBAR_FOLDER="true">FooDir</H3>
    <DL><p>
		<DT><A HREF="https://bar.io/" ADD_DATE="1515361177" ICON="data:image/png;base64,blah==">Bar</A>
        <DT><H3 ADD_DATE="1515126229" LAST_MODIFIED="1592885828">BarDir</H3>
        <DL><p>
			<DT><A HREF="https://qux.io/" ADD_DATE="1515361177" ICON="data:image/png;base64,blah==">Qux</A>
            <DT><H3 ADD_DATE="1516481807" LAST_MODIFIED="1573835459">Qux Dir</H3>
            <DL><p>
				<DT><A HREF="https://bee.io/" ADD_DATE="1515361173" ICON="data:image/png;base64,blahblah==">Bee</A>
`)),
			expBookmarks: []*Bookmark{
				{
					URL:     "https://bar.io/",
					Title:   "Bar",
					AddDate: time.Unix(1515361177, 0),
				},
				{
					URL:     "https://qux.io/",
					Title:   "Qux",
					AddDate: time.Unix(1515361177, 0),
				},
				{
					URL:     "https://bee.io/",
					Title:   "Bee",
					AddDate: time.Unix(1515361173, 0),
				},
			},
			expErrs: []bool{false, false, false},
		},
		{

			name: "Empty",
			reader: bytes.NewReader([]byte(`<!DOCTYPE NETSCAPE-Bookmark-file-1>
<!-- This is an automatically generated file.
     It will be read and overwritten.
     DO NOT EDIT! -->
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks</TITLE>
`)),
			expBookmarks: []*Bookmark{},
			expErrs:      []bool{},
		},
		{
			name: "MalformedBookmarkFile",
			reader: bytes.NewReader([]byte(`<!DOCTYPE NETSCAPE-Bookmark-file-1>
<!-- This is an automatically generated file.
     It will be read and overwritten.
     DO NOT EDIT! -->
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks</TITLE>
<H1>Bookmarks</H1>
<DL><p>
    <DT><H3 ADD_DATE="1512790922" LAST_MODIFIED="1588537285" PERSONAL_TOOLBAR_FOLDER="true">FooDir</H3>
    <DL><p>
		<DT><A HREF="https://bar.io/" ADD_DATE="1515361177" ICON="data:image/png;base64,blah==">Bar</A>
        <DT><H3 ADD_DATE="1515126229" LAST_MODIFIED="1592885828">BarDir</H3>
        <DL><p>
			<DT><A HREF="https://qux.io/" ADD_DATE="junkie" ICON="data:image/png;base64,blah==">Qux</A>
            <DT><H3 ADD_DATE="1516481807" LAST_MODIFIED="1573835459">Qux Dir</H3>
            <DL><p>
				<DT><A HREF="https://bee.io/" ADD_DATE="1515361173" ICON="data:image/png;base64,blahblah==">Bee</A>
`)),
			expBookmarks: []*Bookmark{
				{
					URL:     "https://bar.io/",
					Title:   "Bar",
					AddDate: time.Unix(1515361177, 0),
				},
				(*Bookmark)(nil),
			},
			expErrs: []bool{false, true},
		},
	}
	log := genTstLogger()
	for _, cs := range tcs {
		c := cs
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := NewNetscapeWalker(c.reader, log)
			bs, errs := make([]*Bookmark, 0, len(c.expBookmarks)), make([]bool, 0, len(c.expErrs))
			for {
				b, err := w.Next()
				if err != nil && err == io.EOF {
					break
				}
				bs = append(bs, b)
				errs = append(errs, err != nil)
			}
			assert.Equal(t, c.expBookmarks, bs)
			assert.Equal(t, c.expErrs, errs)
		})
	}
}

func TestWalkerStopEarly(t *testing.T) {
	// get a reader providing infinite bookmark stream :D
	// have a separate goroutine start the walker on the stream
	// main goroutine await the start of walker, call walker's Stop() and await the starter goroutine to exhuast the walker.
	r, log := &infReader{}, genTstLogger()
	walker := NewNetscapeWalker(r, log)
	// start iteration on walker
	started, start, done := false, make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			b, err := walker.Next()
			if !started {
				started = true
				close(start)
			}
			if err != nil {
				assert.Empty(t, b, "no bookmark data should have present on exhausting walker")
				assert.Equalf(t, io.EOF, err, "expect io.EOF on exhausting walker but got %v", err)
				return
			}
			assert.NotEmpty(t, b, "bookmark data should have present when walker is not exhausted")
		}
	}()
	// stop walker after iteration had started
	<-start
	walker.Stop()
	// verify iteration on walker is eventually stopped
	timeout := time.NewTimer(1 * time.Second)
	defer timeout.Stop()
	select {
	case <-done:
	case <-timeout.C:
		t.Error("iteration on walker still running after timeout elapsed, indicating buggy walker stop mechanism")
	}
}

// infReader mimics reading an inifinite byte stream of bookmarks.
type infReader struct {
	curr io.Reader
}

func (r *infReader) Read(p []byte) (int, error) {
	// read data from it into p. if curr exhaust, replace it with a new one
	if r.curr == nil {
		r.curr = r.spare()
	}
	curr := r.curr
	n, err := curr.Read(p)
	if err == io.EOF {
		r.curr = r.spare()
		return n, nil
	}
	return n, err
}

func (r *infReader) spare() io.Reader {
	return bytes.NewReader([]byte(`<DT><A HREF="https://foo/" ADD_DATE="1515361177" ICON="data:image/png;base64,blah==">Foo</A>`))
}
