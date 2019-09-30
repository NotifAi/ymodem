package main

import (
	"time"

	"github.com/vbauerster/mpb/v4"
	"github.com/vbauerster/mpb/v4/decor"

	ytypes "github.com/NotifAi/ymodem/types"
)

type bars struct {
	b    *mpb.Progress
	bars []*mpb.Bar
}

type bar struct {
	b       *mpb.Bar
	startTs time.Time
}

var _ ytypes.Progress = (*bars)(nil)
var _ ytypes.Bar = (*bar)(nil)

func newProgress() ytypes.Progress {
	b := &bars{
		b: mpb.New(mpb.WithWidth(64)),
	}

	return b
}

func (b *bars) Create(name string, ln int) ytypes.Bar {
	br := &bar{
		b: b.b.AddBar(int64(ln),
			mpb.BarClearOnComplete(),
			mpb.PrependDecorators(
				decor.Name(name, decor.WC{W: len(name) + 1, C: decor.DSyncSpaceR}),
				decor.CountersNoUnit("%d / %d", decor.WCSyncWidth),
				decor.OnComplete(decor.Name("", decor.WCSyncSpaceR), " done!"),
			),
			mpb.AppendDecorators(
				decor.Percentage(decor.WC{W: 5}), ),
		),
		startTs: time.Now(),
	}

	return br
}

func (b *bars) Shutdown() {
	for _, b := range b.bars {
		if !b.Completed() {
			b.Abort(false)
		}
	}

	b.b.Wait()
}

func (b *bar) Add(n int) error {
	b.b.IncrBy(n, time.Since(b.startTs))
	return nil
}
