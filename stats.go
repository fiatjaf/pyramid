package main

import (
	"cmp"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"slices"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"github.com/a-h/templ"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
	"github.com/go-analyze/charts"
)

type KindInfo struct {
	Kind  nostr.Kind
	Count uint
}

type AuthorInfo struct {
	Author nostr.PubKey
	Count  uint
}

func getTopKinds(perKind map[nostr.Kind]mmm.KindStats, infosLimit, kindsLimit int) ([]KindInfo, []nostr.Kind) {
	kindinfos := make([]KindInfo, 0, infosLimit)
	for kind, stats := range perKind {
		kindinfos = append(kindinfos, KindInfo{Kind: kind, Count: stats.Total})
	}

	slices.SortFunc(kindinfos, func(a, b KindInfo) int { return cmp.Compare(b.Count, a.Count) })
	if len(kindinfos) > infosLimit {
		kindinfos = kindinfos[:infosLimit]
	}
	kinds := make([]nostr.Kind, 0, kindsLimit)
	for i := range min(len(kindinfos), kindsLimit) {
		kinds = append(kinds, kindinfos[i].Kind)
	}

	return kindinfos, kinds
}

func getTopAuthors(perPrefix map[nostr.PubKey]mmm.PubKeyStats, limit int) []AuthorInfo {
	var authors []AuthorInfo
	for pubkey, stats := range perPrefix {
		authors = append(authors, AuthorInfo{Author: pubkey, Count: stats.Total})
	}

	slices.SortFunc(authors, func(a, b AuthorInfo) int { return cmp.Compare(b.Count, a.Count) })
	if len(authors) > limit {
		authors = authors[:limit]
	}

	return authors
}

func generateWeeklyChart(stats mmm.EventStats, top5Kinds []nostr.Kind, name string) templ.Component {
	if len(stats.PerWeek) == 0 {
		return templ.ComponentFunc(func(ctx context.Context, w io.Writer) (err error) { return nil })
	}

	values := [][]float64{}
	mainValues := []float64{}
	kindsValues := make(map[nostr.Kind][]float64)
	for i := len(stats.PerWeek) - 1; i >= 0; i-- {
		mainValues = append(mainValues, float64(stats.PerWeek[i]))
		for _, kindNum := range top5Kinds {
			val := float64(0)
			if kindStats, ok := stats.PerKind[kindNum]; ok && len(kindStats.PerWeek) > i {
				val = float64(kindStats.PerWeek[i])
			}
			kindsValues[kindNum] = append(kindsValues[kindNum], val)
		}
	}

	labels := make([]string, len(stats.PerWeek))
	labels[0] = fmt.Sprintf("%d weeks ago", len(stats.PerWeek))
	labels[len(stats.PerWeek)-1] = "this week"

	values = append(values, mainValues)
	seriesNames := []string{"all"}

	for kindNum, kindValues := range kindsValues {
		values = append(values, kindValues)
		seriesNames = append(seriesNames, fmt.Sprintf("kind:%d", kindNum))
	}

	// generate the chart
	opt := charts.NewLineChartOptionWithData(values)
	opt.XAxis.Labels = labels
	opt.Legend = charts.LegendOption{
		SeriesNames: seriesNames,
	}
	p := charts.NewPainter(charts.PainterOptions{
		Width:  800,
		Height: 400,
	})
	err := p.LineChart(opt)
	if err != nil {
		return templ.ComponentFunc(func(ctx context.Context, w io.Writer) (err error) { return nil })
	}

	buf, err := p.Bytes()
	if err != nil {
		return templ.ComponentFunc(func(ctx context.Context, w io.Writer) (err error) { return nil })
	}

	// return the image as a base64 data URL
	dataURL := fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(buf))

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) (err error) {
		_, err = fmt.Fprintf(w, `<img src="%s" alt="%s weekly activity chart" class="w-full max-w-4xl mx-auto rounded-lg shadow-md">`, dataURL, name)
		return err
	})
}

var relevantUsers map[string]*relevant

func fillInRelevantUsersMapping() {
	relevantUsers = map[string]*relevant{
		"blossom":   {"blossom", global.IL.Blossom, make([]nostr.PubKey, 0, pyramid.Members.Size()), 0},
		"main":      {"main", global.IL.Main, make([]nostr.PubKey, 0, pyramid.Members.Size()), 0},
		"system":    {"system", global.IL.System, make([]nostr.PubKey, 0, pyramid.Members.Size()), 0},
		"internal":  {"internal", global.IL.Internal, make([]nostr.PubKey, 0, pyramid.Members.Size()), 0},
		"groups":    {"groups", global.IL.Groups, make([]nostr.PubKey, 0, pyramid.Members.Size()), 0},
		"favorites": {"favorites", global.IL.Favorites, make([]nostr.PubKey, 0, pyramid.Members.Size()), 0},
		"popular":   {"popular", global.IL.Popular, make([]nostr.PubKey, 0, pyramid.Members.Size()), 0},
		"uppermost": {"uppermost", global.IL.Uppermost, make([]nostr.PubKey, 0, pyramid.Members.Size()), 0},
		"inbox":     {"inbox", global.IL.Inbox, make([]nostr.PubKey, 0, pyramid.Members.Size()), 0},
		"secret":    {"secret", global.IL.Secret, make([]nostr.PubKey, 0, pyramid.Members.Size()), 0},
		"moderated": {"moderated", global.IL.Moderated, make([]nostr.PubKey, 0, pyramid.Members.Size()), 0},
	}
}

type relevant struct {
	label        string
	store        *mmm.IndexingLayer
	pubkeys      []nostr.PubKey
	lastComputed nostr.Timestamp
}

func (r *relevant) get() []nostr.PubKey {
	if r.lastComputed < nostr.Now()-60*3 /* 3 minutes */ {
		r.recompute()
	}
	return r.pubkeys
}

func (r *relevant) recompute() {
	stats, err := r.store.ComputeStats(mmm.StatsOptions{})
	if err != nil {
		log.Error().Err(err).Msg("failed to compute stats for usersWithEvents")
		return
	}

	newList := make([]nostr.PubKey, 0, len(stats.PerPubKey))
	for pubkey, count := range stats.PerPubKey {
		if count.Total > 0 {
			newList = append(newList, pubkey)
		}
	}

	r.pubkeys = newList
	r.lastComputed = nostr.Now()
}
