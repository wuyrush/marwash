package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"go.uber.org/zap"
)

type Washer struct {
	walker  Walker
	pinger  Pinger
	log     *zap.SugaredLogger
	walked  chan *Bookmark // vends bookmarks from walker for washing
	washed  chan *Result   // vends washed bookmarks
	cquota  chan struct{}  // limits concurrency
	done    chan struct{}  // signals termination
	started bool
}

// StartWashTillDone creates the washer with in and doer then starts it, piping the wash result to out.
// It exits until it either finishes iterating the washer or receives signals from OS
func StartWashTillDone(in io.Reader, out io.Writer, doer Doer, cquota int, log *zap.SugaredLogger) {
	walker := NewNetscapeWalker(in, log)
	defer walker.Stop()
	pinger := NewHTTPinger(doer, log)
	washer := NewWasher(walker, pinger, log, cquota)
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	washed, done := make(chan Result), make(chan struct{})
	go func() {
		defer close(washed)
		for {
			bmk, err := washer.Next()
			if err == io.EOF {
				log.Debug("finished iterating washer")
				return
			}
			select {
			case washed <- Result{B: bmk, E: err}:
			case <-done:
				log.Debug("aborted iterating washer")
				return
			}
		}
	}()

	sb := &strings.Builder{}
	defer close(done)
	defer washer.Stop()
	for {
		select {
		case r, ok := <-washed:
			if !ok {
				log.Debug("wash done")
				return
			}
			fmt.Fprintf(sb, "%s\t%s", r.B.Status, r.B.URL)
			if r.E != nil {
				fmt.Fprintf(sb, "\t%s", r.E)
			}
			fmt.Fprintln(out, sb.String())
		case s := <-sigs:
			log.Debugw("received system signal. Exit", "signal", s)
			return
		}
		sb.Reset()
	}
}

// NewWasher creates a new Washer value. Specify cquota to limit the max concurrency Washer can consume.
func NewWasher(walker Walker, pinger Pinger, log *zap.SugaredLogger, cquota int) *Washer {
	return &Washer{
		walker: walker,
		pinger: pinger,
		log:    log,
		walked: make(chan *Bookmark),
		washed: make(chan *Result),
		cquota: make(chan struct{}, cquota),
		done:   make(chan struct{}),
	}
}

// Next returns the next bookmark whose liveness had been checked, aka "washed" by washer. It returns io.EOF
// when there is no more bookmark left, or a non-EOF error when washer encounter any during washing.
func (w *Washer) Next() (*Bookmark, error) {
	if !w.started {
		w.log.Debug("start washing")
		go w.wash()
		w.started = true
	}
	res, ok := <-w.washed
	if !ok {
		return nil, io.EOF
	}
	return res.B, res.E
}

// Stop stops washer. Consecutively calling Next() after calling Stop() *eventually* returns io.EOF
func (w *Washer) Stop() { close(w.done) }

func (w *Washer) wash() {
	var wkerr error
	var wg sync.WaitGroup
	defer func() {
		// wait till all goroutines sending washed bookmarks exit
		wg.Wait()
		if wkerr != nil {
			select {
			case w.washed <- &Result{B: nil, E: wkerr}:
			case <-w.done:
				return
			}
		}
		close(w.washed)
	}()
	// fetch bookmarks to process from walker in async
	go func() {
		defer close(w.walked)
		for {
			var bmk *Bookmark
			bmk, wkerr = w.walker.Next()
			if wkerr != nil {
				return
			}
			select {
			case w.walked <- bmk:
			case <-w.done:
				return
			}
		}
	}()
	for {
		select {
		case bmk, ok := <-w.walked:
			if !ok {
				return
			}
			// wash fetched bookmark in async
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case w.cquota <- struct{}{}:
					defer func() { <-w.cquota }()
				case <-w.done:
					return
				}
				status, err := w.pinger.Ping(bmk.URL)
				bmk.Status = status
				select {
				case w.washed <- &Result{B: bmk, E: err}:
				case <-w.done:
				}
			}()
		case <-w.done:
			return
		}
	}
}
