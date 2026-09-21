package app

import (
	"fmt"
	"io"
	"time"
)

// progress prints a simple upload progress line when stdout is interactive.
type progress struct {
	out         io.Writer
	interactive bool
	counter     *countingReader
	done        chan struct{}
	start       time.Time
}

func newProgress(out io.Writer, interactive bool) *progress {
	return &progress{out: out, interactive: interactive, done: make(chan struct{})}
}

func (p *progress) attach(c *countingReader) {
	p.counter = c
	if !p.interactive {
		return
	}
	p.start = time.Now()
	go p.loop()
}

func (p *progress) loop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			if p.counter == nil {
				continue
			}
			n := p.counter.n
			elapsed := time.Since(p.start).Seconds()
			if elapsed <= 0 {
				continue
			}
			fmt.Fprintf(p.out, "\rUploading... %s (%.1f MiB/s)", humanBytes(n), float64(n)/elapsed/1024/1024)
		}
	}
}

func (p *progress) stop() {
	if !p.interactive {
		return
	}
	close(p.done)
	fmt.Fprint(p.out, "\r")
}
