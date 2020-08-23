package main

import (
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
	// Stop stops and exits the walk. Consecutive calls to Next() after calling Stop() *eventually* returns io.EOF
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
	w.log = log
	return w
}

func (w *NetscapeWalker) Next() (*Bookmark, error) {
	// no need to be goroutine-safe for now
	if !w.started {
		w.log.Debug("start walking")
		go w.walk()
		w.started = true
	}
	r, ok := <-w.wchan
	if !ok {
		return nil, io.EOF
	}
	return r.B, r.E
}

func (w *NetscapeWalker) Stop() {
	close(w.done)
}

func (w *NetscapeWalker) walk() {
	log := w.log
	wchan := w.wchan
	defer close(wchan)
	z := w.tokenizer
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
			log.Debugw("get anchor start tag with attributes", "data", t.Data, "attr", t.Attr)
			var err error
			bmk, err = genBookmark(t.Attr)
			log.Infow("created bookmark", "bookmark", bmk, "err", err)
			if err != nil {
				select {
				case wchan <- &walkResult{nil, err}:
				case <-w.done:
				}
				return
			}
		case html.EndTagToken:
			t := z.Token()
			log.Debugw("get anchor end tag", "containsBookmark", bmk != nil)
			if t.Data != anchorTag || bmk == nil {
				continue
			}
			select {
			case wchan <- &walkResult{bmk, nil}:
				log.Infow("dispatched bookmark", "bookmark", bmk)
			case <-w.done:
				return
			}
			// reset
			bmk = nil
		case html.TextToken:
			// assume bookmark title text has been url-escaped
			if bmk != nil {
				bmk.Title = z.Token().Data
				log.Debugw("assgined bookmark title", "bookmark", bmk)
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
