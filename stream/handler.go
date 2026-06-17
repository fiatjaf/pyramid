package stream

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"fiatjaf.com/nostr/khatru"
	"github.com/gwuhaolin/livego/protocol/hls"
	"github.com/gwuhaolin/livego/protocol/rtmp"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	L       = global.Log.With().Str("module", "stream").Logger()
	Handler = &MuxHandler{}

	hlsServer     *hls.Server
	rtmpStream    *rtmp.RtmpStream
	rtmpCtxCancel context.CancelFunc
)

func Init(relay *khatru.Relay) {
	L.Debug().Msg("initializing stream service")

	if !global.Settings.Stream.Enabled {
		setupDisabled()
	} else {
		setupEnabled()
	}
}

func Restart() {
	setupDisabled()
	setupEnabled()
}

func setupDisabled() {
	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /stream/enable", enableHandler)
	Handler.mux.HandleFunc("/stream/", pageHandler)
	if rtmpCtxCancel != nil {
		rtmpCtxCancel()
	}
	L.Debug().Msg("stream service disabled")
}

func setupEnabled() {
	hlsServer = hls.NewServer()
	hlsServer.BasePath = "/stream"
	rtmpStream = rtmp.NewRtmpStream()

	rtmpCtx, cancel := context.WithCancel(context.Background())
	if err := startRtmp(rtmpCtx, rtmpStream, hlsServer); err != nil {
		L.Warn().Err(err).Msg("failed to start rtmp")
	}
	rtmpCtxCancel = cancel

	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /stream/disable", disableHandler)
	Handler.mux.HandleFunc("/stream/live/", hlsServer.Handle)
	Handler.mux.HandleFunc("/stream/", pageHandler)
	L.Debug().Msg("stream service enabled")
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}

	global.Settings.Stream.Enabled = true
	if global.Settings.Stream.Salt == "" {
		global.Settings.Stream.Salt = global.RandomString(12)
	}

	if err := global.SaveUserSettings(); err != nil {
		L.Error().Err(err).Msg("failed to save settings")
		http.Error(w, "failed to save settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	setupEnabled()
	http.Redirect(w, r, "/stream/", http.StatusFound)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}

	global.Settings.Stream.Enabled = false
	if err := global.SaveUserSettings(); err != nil {
		L.Error().Err(err).Msg("failed to save settings")
		http.Error(w, "failed to save settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	setupDisabled()
	http.Redirect(w, r, "/stream/", http.StatusFound)
}

type MuxHandler struct {
	mux *http.ServeMux
}

func (mh *MuxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mh.mux.ServeHTTP(w, r)
}

func startRtmp(ctx context.Context, stream *rtmp.RtmpStream, hlsServer *hls.Server) error {
	var rtmpListen net.Listener
	rtmpListen, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", global.Settings.Stream.Port))
	if err != nil {
		return err
	}

	rtmpServer := rtmp.NewRtmpServer(stream, hlsServer)

	go func() {
		<-ctx.Done()
		rtmpListen.Close()
	}()

	go func() {
		defer func() {
			if err := recover(); err != nil {
				L.Error().Any("err", err).Msg("RTMP server panic")
			}
		}()
		rtmpServer.Serve(rtmpListen)
	}()

	return nil
}
