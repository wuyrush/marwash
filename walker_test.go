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
