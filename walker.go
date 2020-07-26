package main

import (
	"bytes"
	"io"
	"strconv"
	"time"

	"go.uber.org/zap"
	"golang.org/x/net/html"
)

const (
	anchorTag = "a"
)

type Bookmark struct {
	URL     string
	Title   string
	AddDate time.Time
	Status  PingStatus
	// TODO: we may want other attributes in the future
}

// Walker walks through input text stream of bookmarks, assuming the input text stream is already utf-8 encoded HTML.
type Walker interface {
	// Next walks to and returns the next bookmark. It returns io.EOF when walking to the end of text
	// stream, or a non-EOF error when error happened during walk.
	Next() (*Bookmark, error)
	// Stop stops and exits the walk. A call to Next after calling Stop() has no side effect but returns io.EOF
	Stop()
}

// NetscapeWalker walks text stream in Netscape Bookmark File Format.
type NetscapeWalker struct {
	tokenizer *html.Tokenizer
	wchan     chan *walkResult
	done      chan struct{}
	log       *zap.SugaredLogger
	started   bool
}

type walkResult struct {
	B *Bookmark
	E error
}

func NewNetscapeWalker(r io.Reader, log *zap.SugaredLogger) *NetscapeWalker {
	// keep constructor simple, avoid heavy operations like spinning up goroutines etc
	t := html.NewTokenizer(r)
	w := &NetscapeWalker{}
	w.tokenizer = t
	w.wchan = make(chan *walkResult)
	w.done = make(chan struct{})
	return w
}

func (w *NetscapeWalker) Next() (*Bookmark, error) {
	// no need to be goroutine-safe for now
	if !w.started {
		go w.walk()
		w.started = true
	}
	r, ok := <-w.wchan
	if !ok {
		return nil, io.EOF
	}
	return r.B, r.E
}

func (w *NetscapeWalker) walk() {
	wchan := w.wchan
	defer close(wchan)
	z := w.tokenizer
	anchor := false
	var bmk *Bookmark
	for {
		switch tt := z.Next(); tt {
		case html.ErrorToken:
			select {
			case wchan <- &walkResult{nil, z.Err()}:
			case <-w.done:
			}
			return
		case html.StartTagToken:
			t := z.Token()
			if t.Data != anchorTag || len(t.Attr) == 0 {
				continue
			}
			anchor = true
			var err error
			bmk, err = genBookmark(t.Attr)
			if err != nil {
				select {
				case wchan <- &walkResult{nil, err}:
				case <-w.done:
				}
				return
			}
		case html.EndTagToken:
			t := z.Token()
			if t.Data != anchorTag || bmk == nil {
				continue
			}
			select {
			case wchan <- &walkResult{bmk, nil}:
			case <-w.done:
				return
			}
			// reset
			bmk, anchor = nil, false
		case html.TextToken:
			// assume bookmark title text has been url-escaped
			if anchor && bmk != nil {
				bmk.Title = z.Token().Data
			}
		}
	}
}

func genBookmark(attr []html.Attribute) (*Bookmark, error) {
	var url string
	var addDate time.Time
	for _, a := range attr {
		switch a.Key {
		case "href":
			url = a.Val
		case "add_date":
			addDateSeconds, err := strconv.ParseInt(a.Val, 10, 64)
			if err != nil {
				return nil, err
			}
			addDate = time.Unix(addDateSeconds, 0)
		}
	}
	return &Bookmark{URL: url, AddDate: addDate}, nil
}

func (w *NetscapeWalker) Stop() {
	close(w.done)
}

func main() {
	txt := `
<!DOCTYPE NETSCAPE-Bookmark-file-1>
<!-- This is an automatically generated file.
     It will be read and overwritten.
     DO NOT EDIT! -->
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks</TITLE>
<H1>Bookmarks</H1>
<DL><p>
    <DT><H3 ADD_DATE="1512790922" LAST_MODIFIED="1588537285" PERSONAL_TOOLBAR_FOLDER="true">Bookmarks Bar</H3>
    <DL><p>
        <DT><H3 ADD_DATE="1515126229" LAST_MODIFIED="1592885828">Go</H3>
        <DL><p>
            <DT><H3 ADD_DATE="1516481807" LAST_MODIFIED="1573835459">Noice tutorials</H3>
            <DL><p>
                <DT><A HREF="https://appliedgo.net/" ADD_DATE="1515361177" ICON="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAADjklEQVQ4jV2TW2wUZRzFz/d9s9PZaXf20gJLW3pj20SJUlOqQrHUBxKqJryIvhCjYvRViQ/q0zRBisaooCQKrRgbUaGE1IRAI4mohUobSlNbte1u29122V62l93upbM7M3+fIOp5P7/feTkM/4uvtVVbuXp1Y8/7n+x31T20kxUVyvlMOpOcDgXHvvuqP3PnTgwADhGJi21txAAwAPSo/kG5u6r6nOorCUDwNamipl51e+AgG6aZh53JwFxPLqQi4Ys33z56Ijs3EQUABl2XoOvmgd6+dzOzkeOhyz+g6VQH/HPTWCNYqyWlpDoEy3PBbYfMVFXF6tBAOJ8zby4M3vr8wYLqjm8rH66pu06SVP2yMOzn6mqknGXj2N9hjG2pgDseQ1kui2ih28qoRVLo7Glsbmqe5wQAus5f6PmmKJ1aT1S6NdEaqOST8RV2obub7dMUpkZCrH2bDycad+CoYgl1NW5WvnjYdDid90QbgEOGoTzz6is9pX7/rqH1DatFlbhLktjyzBSmVA1lBQ54FqIsZphoDFRjeiXJJyVnfrr9vYP8s5MnP77Y359dTBunWmQiFyz7w1gCfyRSMPa1oq8sgKR3E3qvXEF2aZHljRxFbTBh5ROZSGRW2tO0+63OzrOGpmk1GYsgCYmH/VX4Mh6F0teNkaEQ0s17ceDwa8jLjI6NTNjR7TuYFBwfvTcxsSxNTgZvNDQ0vONxKvh5PEjL/jpRBIICC0eUWRwsDkJOMpyjx3DNVlDg90ETElucme4FQKL70qXzk+H57xWtxNlYW7FzZHXdNjw+lnAUAvCwvVW16Kp8GkbxVnJJgmRZ5tn4Qjp0uevN1bt3VzgRmW88nk4Fcre2K5QTzWaaZdcTUAVjEUXDebMYa96tJEAwbdtmior46Ni1UGdnEEScM8Yozzc/Ue4V3sGhgdu7NnlF6fwsLM5paUs5RgOPgAnBmGkySBK3lmLsuHPo2Z/an28BYzb79w+q6p+s6vqobTgGoX3t3kYul8bJMkGcExHZstvLo7f7b3Skut3zG2L4qddPH+H3y6TrfGb490hPaC5a79NYdTxKWcZBDCAAYJzIzDEjlbxQ+9IXu8/8Nt4GAA8ATNdtADTo9ifDOZv2mymYy4uA5AAjEJOEyMSXzETfrwMAcl1d1yP/AYCIAyDyeMI/Gpz9QgUkFxaBLBsEIiHLzEylpv468+mf950A8A887K7KvWHb/gAAAABJRU5ErkJggg==">Applied Go · Applied Go</A>
                <DT><A HREF="https://gobyexample.com/" ADD_DATE="1515361173" ICON="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAIAAACQkWg2AAABtElEQVQ4jZWSsW4aQRCGZ+b2uEOiuQOUw6kQClbcxBJxEVe2xBOE1LFTOQqpnTcILumdEjmS8x4XemMDPqdDMRXNARvulp0Ul5ADJ0Wmmln9v/b7dwfL5TL8T9HGjIiI+LD/uwERoyiKoigZlVJxHBPRPw1Syq2tx57naa2Z2XGcQqEwm83SGsNxHAAgoul0+q7ZfHtyMplM+v3+Uut6vf7h9NR1Xd/3bdtm5rUbiGhv7/nZWevi4nM2a2dt+8vl5cdWa3//hTCMTSQiGo/Hg8FwZ+epYVASmJkrlUoQ3H6/v0ekNSSt9eujo8PDg06nE4ZTwzCYGQFipRqNV7lcLgiCzdDz+VwpZZpmwpq82nK5RMRMJrOJJIT4dH7e613v7j6TUhIREv2Qslar3dxct9tt43eMXwat9SPP296u3t19IzKYGZgZYDAcPqlWS6WS1notAyIuFot8Pn98/IYIrq56SqmXjcb7ZvOr73e7XcuyElRM75KUslgsAkAYhszsuq4QYjQa2bb9ZxvSBiKK4xgAEmKlFDNblrXiAQCR/nattRACIOEH0zSTw7RmzbCSPuxX9RP2E8MfDbaP4AAAAABJRU5ErkJggg==">Go by Example</A>
	`
	r := bytes.NewReader([]byte(txt))
	log, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	sl := log.Sugar()
	var w Walker = NewNetscapeWalker(r, sl)
	defer w.Stop()
	for {
		b, err := w.Next()
		sl.Debugf("%#v, %#v", b, err)
		if err != nil {
			switch err {
			case io.EOF:
				return
			default:
				panic(err)
			}
		}
	}
}
