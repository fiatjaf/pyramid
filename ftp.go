package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	blossom_lib "fiatjaf.com/nostr/nipb0/blossom"
	"github.com/fiatjaf/pyramid/blossom"
	"github.com/fiatjaf/pyramid/global"
	"github.com/liamg/magic"
	"github.com/mailru/easyjson"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

var sftpVFS = SFTPVirtualFileSystem{}
var sftpListener net.Listener

func startSFTP() error {
	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "admin" && string(pass) == global.Settings.FTP.Password {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected for %q", c.User())
		},
	}

	privateKey, err := getSFTPHostKey()
	if err != nil {
		return fmt.Errorf("failed to generate host key: %w", err)
	}

	config.AddHostKey(privateKey)
	addr := net.JoinHostPort(global.S.Host, global.S.SFTPPort)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	sftpListener = listener
	log.Info().Msgf("SFTP server listening on %s", addr)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Error().Err(err).Msg("failed to accept incoming connection")
				if sftpListener == nil {
					// server has stopped
					return
				}

				continue
			}

			go handleSFTP(conn, config)
		}
	}()

	return nil
}

func stopSFTP() {
	if sftpListener != nil {
		sftpListener.Close()
		sftpListener = nil
		log.Info().Msg("SFTP server stopped")
	}
}

func handleSFTP(conn net.Conn, config *ssh.ServerConfig) {
	defer conn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		log.Error().Err(err).Msg("failed to handshake")
		return
	}
	defer sshConn.Close()

	log.Info().Stringer("from", sshConn.RemoteAddr()).Str("client", string(sshConn.ClientVersion())).
		Msg("new SFTP connection")

	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			log.Error().Err(err).Msg("could not accept channel")
			continue
		}

		go func(in <-chan *ssh.Request) {
			for req := range in {
				if req.Type == "subsystem" && string(req.Payload[4:]) == "sftp" {
					req.Reply(true, nil)

					server := sftp.NewRequestServer(channel, sftp.Handlers{
						FileGet:  sftpVFS,
						FilePut:  sftpVFS,
						FileCmd:  sftpVFS,
						FileList: sftpVFS,
					})
					if err := server.Serve(); err != nil && err != io.EOF {
						log.Error().Err(err).Msg("SFTP server completed with error")
					}
					server.Close()
				} else {
					req.Reply(false, nil)
				}
			}
		}(requests)
	}
}

func getSFTPHostKey() (ssh.Signer, error) {
	keyPath := filepath.Join(global.S.DataPath, "sftp_host_key")

	if _, err := os.Stat(keyPath); err == nil {
		keyData, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, err
		}
		return ssh.ParsePrivateKey(keyData)
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}

	if err := os.WriteFile(keyPath, pem.EncodeToMemory(privateKeyPEM), 0600); err != nil {
		return nil, err
	}

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return nil, err
	}

	return signer, nil
}

func getSSHPublicKey() string {
	key, err := getSFTPHostKey()
	if err != nil {
		return ""
	}

	pub := key.PublicKey()
	if pub == nil {
		return ""
	}

	hash := sha256.Sum256(pub.Marshal())
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(hash[:])
}

func getEventStore(storeName string) *mmm.IndexingLayer {
	if relevant, ok := relevantUsers[storeName]; ok {
		return relevant.store
	}
	return nil
}

type SFTPVirtualFileSystem struct{}

// FileReader interface implementation (for FileGet)
func (vfs SFTPVirtualFileSystem) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	r.Filepath = strings.TrimPrefix(r.Filepath, "/")

	path := strings.Split(r.Filepath, "/")
	if len(path) < 2 {
		return nil, os.ErrPermission
	}

	switch path[0] {
	case "blossom":
		if len(path) != 3 {
			return nil, os.ErrNotExist
		}

		// pubkey, err := parsePubKey(path[1])
		// if err != nil {
		// 	return nil, err
		// }

		spl := strings.Split(path[2], ".")
		hash := spl[0]
		ext := "." + spl[1]

		blob, _ := blossom.BlobIndex.Get(r.Context(), hash)
		if blob == nil {
			return nil, os.ErrNotExist
		}

		reader, _, err := blossom.Server.LoadBlob(r.Context(), hash, ext)
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(reader)
		return bytes.NewReader(data), err
	default:
		// any of the relays
		store := getEventStore(path[0])
		if store == nil {
			return nil, os.ErrNotExist
		}
		spl := strings.Split(path[len(path)-1], ".")
		id := spl[0]

		if id, err := nostr.IDFromHex(id); err == nil {
			for evt := range store.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
				data, _ := json.MarshalIndent(evt, "", "  ")
				return bytes.NewReader(data), nil
			}
		} else {
			return nil, err
		}
	}

	return nil, os.ErrNotExist
}

// FileWriter interface implementation (for FilePut)
func (vfs SFTPVirtualFileSystem) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	r.Filepath = strings.TrimPrefix(r.Filepath, "/")

	path := strings.Split(r.Filepath, "/")
	if len(path) < 2 {
		return nil, os.ErrNotExist
	}
	if _, ok := relevantUsers[path[0]]; !ok {
		return nil, os.ErrNotExist
	}

	return &SFTPWriterAt{
		ctx:  r.Context(),
		path: path,
	}, nil
}

// FileCmder interface implementation (for FileCmd)
func (vfs SFTPVirtualFileSystem) Filecmd(r *sftp.Request) error {
	r.Filepath = strings.TrimPrefix(r.Filepath, "/")

	path := strings.Split(r.Filepath, "/")
	if len(path) < 3 {
		return os.ErrNotExist
	}
	if _, ok := relevantUsers[path[0]]; !ok {
		return os.ErrNotExist
	}

	pubkey, err := parsePubKey(path[1])
	if err != nil {
		return err
	}

	switch r.Method {
	case "Mkdir":
		return fmt.Errorf("mkdir not supported")

	case "Rmdir":
		return fmt.Errorf("rmdir not supported")

	case "Remove":
		log.Info().Str("subsystem", path[0]).Str("pubkey", pubkey.Hex()).Str("target", path[2]).
			Msg("deleting via SFTP")

		switch path[0] {
		case "blossom":
			spl := strings.Split(path[2], ".")
			hash := spl[0]
			ext := "." + spl[1]

			// delete entry for this pubkey
			if err := blossom.BlobIndex.Delete(r.Context(), hash, pubkey); err != nil {
				return fmt.Errorf("delete of blob entry failed: %s", err)
			}

			// we will actually only delete the file if no one else owns it
			if blob, err := blossom.BlobIndex.Get(r.Context(), hash); err == nil && blob == nil {
				if err := blossom.Server.DeleteBlob(r.Context(), hash, ext); err != nil {
					return fmt.Errorf("failed to delete blob: %s", err)
				}
			}

			return nil
		default:
			// main relay or any of the other relays
			spl := strings.Split(path[len(path)-1], ".")
			id, err := nostr.IDFromHex(spl[0])
			if err != nil {
				return err
			}

			store := getEventStore(path[0])
			if store == nil {
				return os.ErrNotExist
			}
			return store.DeleteEvent(id)
		}

	case "Rename":
		return fmt.Errorf("rename not supported")

	case "Setstat":
		return nil

	case "Symlink":
		return errors.New("symlinks not supported")

	default:
		return errors.New("unsupported command: " + r.Method)
	}
}

// FileLister interface implementation (for FileList)
func (vfs SFTPVirtualFileSystem) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	r.Filepath = strings.TrimPrefix(r.Filepath, "/")
	if r.Filepath == "" {
		return SFTPListerAt{r.Context(), nil}, nil
	}

	path := strings.Split(r.Filepath, "/")
	if _, ok := relevantUsers[path[0]]; !ok {
		return nil, os.ErrNotExist
	}

	switch r.Method {
	case "List":
		return SFTPListerAt{r.Context(), path}, nil

	case "Stat":
		return SFTPListerAt{r.Context(), path}, nil

	case "Readlink":
		return nil, errors.New("symlinks not supported")

	default:
		return nil, errors.New("unsupported method: " + r.Method)
	}
}

type SFTPWriterAt struct {
	ctx  context.Context
	path []string

	// partial writes will be kept here
	buf []byte
	ext string
}

func (w *SFTPWriterAt) WriteAt(data []byte, offset int64) (n int, err error) {
	log.Info().Strs("path", w.path).Int64("offset", offset).Int("size", len(data)).Msg("SFTP write")

	if len(w.path) < 2 {
		return 0, os.ErrNotExist
	}

	// this is for getting/updating the list of users we'll list as directories
	relevant, ok := relevantUsers[w.path[0]]
	if !ok {
		return 0, fmt.Errorf("must operate on blossom or events, got '%s', %v", w.path[0], relevantUsers)
	}

	var pubkey nostr.PubKey
	if len(w.path) == 3 {
		// take the pubkey from the path
		pubkey, err = parsePubKey(w.path[1])
		if err != nil {
			return 0, err
		}
	} else if len(w.path) == 2 {
		// if a blob is just being pushed without a path, assume it will be related to the relay root key
		pubkey = global.Settings.RelayInternalSecretKey.Public()
	}
	filename := w.path[len(w.path)-1]

	switch w.path[0] {
	case "blossom":
		if offset == 0 {
			// try to figure out the file type
			spl := strings.Split(filename, ".")
			var ext string
			if len(spl) > 1 {
				ext = spl[len(spl)-1]
			}

			// read first bytes of upload so we can find out the filetype
			if ft, _ := magic.Lookup(data); ft != nil {
				if ft.Extension == "zip" && ext == "apk" {
					ext = ".apk"
				} else {
					ext = "." + ft.Extension
				}
			} else {
				// otherwise trust the extension in the filename
				ext = "." + ext
			}

			w.ext = ext
		}

		w.buf = append(w.buf, data...)

		if len(data) < 16384 {
			// this is final
			// (in the edge case of the last part of a file being exactly 16384 we will just fail, unfortunately)
			hash32 := sha256.Sum256(w.buf)
			hash := hex.EncodeToString(hash32[:])

			blob := blossom_lib.BlobDescriptor{
				URL:      global.Settings.HTTPScheme() + global.Settings.Domain + "/" + hash + w.ext,
				SHA256:   hash,
				Size:     len(w.buf),
				Type:     mime.TypeByExtension(w.ext),
				Uploaded: nostr.Now(),
			}
			if err := blossom.BlobIndex.Keep(w.ctx, blob, pubkey); err != nil {
				return 0, fmt.Errorf("fail to save blob: %w", err)
			}

			// save actual blob
			if err := blossom.Server.StoreBlob(w.ctx, hash, w.ext, w.buf); err != nil {
				return 0, fmt.Errorf("failed to save: %w", err)
			}

			global.FiveSecondsDebouncer(relevant.recompute)
		}

		return len(data), nil
	default:
		// main relay or any of the other relays
		w.buf = append(w.buf, data...)

		if len(data) < 16384 {
			// this is final
			// (in the edge case of the last part of an event being exactly 16384 we will just fail silently)
			evt := nostr.Event{}
			if err := easyjson.Unmarshal(w.buf, &evt); err != nil {
				return 0, err
			}
			if evt.PubKey != pubkey {
				if len(w.path) == 2 {
					// a pubkey wasn't specified in the path.
					// in this case we can probably just take the events and assign them to whoever is
					// their owner without checking
				} else {
					// but if a pubkey was specified then we check for that and should fail
					return 0, fmt.Errorf("event pubkey %s doesn't match expected %s", evt.PubKey.Hex(), pubkey.Hex())
				}
			}
			if !evt.VerifySignature() {
				return 0, fmt.Errorf("event signature is invalid")
			}

			store := getEventStore(w.path[0])
			if store == nil {
				return 0, fmt.Errorf("invalid store: %s", w.path[0])
			}
			if err := store.SaveEvent(evt); err != nil {
				return 0, fmt.Errorf("failed to save: %w", err)
			}

			relay.BroadcastEvent(evt)
			global.FiveSecondsDebouncer(relevant.recompute)
		}

		return len(data), nil
	}
}

type SFTPListerAt struct {
	ctx  context.Context
	path []string
}

func (l SFTPListerAt) ListAt(ls []os.FileInfo, offset int64) (int, error) {
	log.Info().Strs("path", l.path).Int64("offset", offset).Msg("SFTP list")

	if l.path == nil {
		storeNames := make([]string, 0, len(relevantUsers))
		for name := range relevantUsers {
			storeNames = append(storeNames, name)
		}

		if offset >= int64(len(storeNames)) {
			return 0, nil
		}

		count := 0
		for i, name := range storeNames {
			if int64(i) < offset {
				continue
			}
			if count >= len(ls) {
				break
			}
			ls[count] = SFTPVirtualDirEntry(name)
			count++
		}

		return count, nil
	}

	n := 0

	// list all relevant users
	if len(l.path) == 1 {
		// this is for getting/updating the list of users we'll list as directories
		relevant, ok := relevantUsers[l.path[0]]
		if !ok {
			return 0, fmt.Errorf("must operate on blossom or events, got '%s', %v", l.path[0], relevantUsers)
		}

		for _, pubkey := range relevant.get() {
			if offset > 0 {
				offset--
				continue
			}

			ls[n] = SFTPVirtualDirEntry(pubkey.Hex())
			n++

			if n >= len(ls) {
				break
			}
		}
		return n, nil
	}

	if len(l.path) == 2 {
		pubkey, err := parsePubKey(l.path[1])
		if err != nil {
			return 0, err
		}
		switch l.path[0] {
		case "blossom":
			for blob := range blossom.BlobIndex.List(l.ctx, pubkey) {
				if offset > 0 {
					offset--
					continue
				}

				ls[n] = SFTPVirtualFileBlob{
					SHA256:    blob.SHA256,
					SizeBytes: int64(blob.Size),
					Uploaded:  blob.Uploaded,
					Mimetype:  blob.Type,
				}
				n++

				if n >= len(ls) {
					break
				}
			}
			return n, nil
		default:
			// any of the relays
			filter := nostr.Filter{
				Limit:   len(ls),
				Authors: []nostr.PubKey{pubkey},
			}

			store := getEventStore(l.path[0])
			if store == nil {
				return 0, os.ErrNotExist
			}

			for evt := range store.QueryEvents(filter, 100+int(offset)) {
				if offset > 0 {
					offset--
					continue
				}

				ls[n] = SFTPVirtualFileEvent(evt)
				n++
			}
			return n, nil
		}
	}

	return 0, os.ErrNotExist
}

type SFTPVirtualDirEntry string

func (fi SFTPVirtualDirEntry) Name() string       { return string(fi) }
func (fi SFTPVirtualDirEntry) Size() int64        { return 0 }
func (fi SFTPVirtualDirEntry) Mode() fs.FileMode  { return fs.ModeDir }
func (fi SFTPVirtualDirEntry) ModTime() time.Time { return time.Unix(0, 0) }
func (fi SFTPVirtualDirEntry) IsDir() bool        { return true }
func (fi SFTPVirtualDirEntry) Sys() any           { return nil }

type SFTPVirtualFileEvent nostr.Event

func (evt SFTPVirtualFileEvent) Name() string { return evt.ID.Hex() + ".json" }
func (evt SFTPVirtualFileEvent) Size() int64 {
	j, _ := easyjson.Marshal(nostr.Event(evt))
	return int64(len(j))
}
func (evt SFTPVirtualFileEvent) Mode() fs.FileMode  { return 0444 }
func (evt SFTPVirtualFileEvent) ModTime() time.Time { return evt.CreatedAt.Time() }
func (evt SFTPVirtualFileEvent) IsDir() bool        { return false }
func (evt SFTPVirtualFileEvent) Sys() any           { return nil }

type SFTPVirtualFileBlob struct {
	SHA256    string
	Mimetype  string
	SizeBytes int64
	Uploaded  nostr.Timestamp
}

func (blob SFTPVirtualFileBlob) Name() string {
	return blob.SHA256 + blossom_lib.GetExtension(blob.Mimetype)
}
func (blob SFTPVirtualFileBlob) Size() int64        { return int64(blob.SizeBytes) }
func (blob SFTPVirtualFileBlob) Mode() fs.FileMode  { return 0444 }
func (blob SFTPVirtualFileBlob) ModTime() time.Time { return blob.Uploaded.Time() }
func (blob SFTPVirtualFileBlob) IsDir() bool        { return false }
func (blob SFTPVirtualFileBlob) Sys() any           { return nil }
