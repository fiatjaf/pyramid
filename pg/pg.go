package pg

import (
	"context"
	"fmt"
	"iter"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"fiatjaf.com/nostr"

	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/rs/zerolog"

	pgparser "github.com/auxten/postgresql-parser/pkg/sql/parser"
	"github.com/auxten/postgresql-parser/pkg/sql/sem/tree"
)

// osGetenv is split out so we don't pull os into the imports of every test
// that links this package statically.
var osGetenv = os.Getenv

// EventStore is what pyramid exposes for each indexing layer. Any nostr
// eventstore.Database (the mmm.IndexingLayer included) implements it.
type EventStore interface {
	QueryEvents(filter nostr.Filter, maxLimit int) iter.Seq[nostr.Event]
	CountEvents(filter nostr.Filter) (uint32, error)
}

// Layer is a named virtual table backed by an [EventStore].
type Layer struct {
	// Name is the SQL identifier (underscores, never dashes) of the table.
	Name string

	// Description is a short human readable description, used in \d comments.
	Description string

	// Store is the underlying event store. May be nil if not active;
	// queries against it return an empty result.
	Store EventStore
}

// Server is the pgwire server wrapper.
type Server struct {
	Log    zerolog.Logger
	Layers []Layer
	Host   string
	Port   int
}

// LayerByName resolves a SQL identifier into a layer. Matching is case
// insensitive and treats - and _ as interchangeable.
func (s *Server) LayerByName(name string) *Layer {
	want := normalizeIdent(name)
	for i := range s.Layers {
		if normalizeIdent(s.Layers[i].Name) == want {
			return &s.Layers[i]
		}
	}
	return nil
}

// normalizeIdent lowercases a SQL identifier and replaces dashes with
// underscores so SQL-exposed layer names like "pending-access" work.
func normalizeIdent(name string) string {
	return strings.ReplaceAll(strings.ToLower(name), "-", "_")
}

var _ = strconv.Itoa

// debugSQL enables printing every incoming query and its parsed AST. Toggled
// via PYRAMID_PG_DEBUG=1 at runtime.
var debugSQL = osGetenv("PYRAMID_PG_DEBUG") == "1"

// Start binds and accepts connections.
func (s *Server) Start(ctx context.Context) error {
	// populate the catalog OID-reverse map used by introspect handlers.
	layersGlobal = layersGlobal[:0]
	for i := range s.Layers {
		layersGlobal = append(layersGlobal, s.Layers[i].Name)
	}

	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("pgwire listen on %s: %w", addr, err)
	}
	s.Log.Info().Str("addr", addr).Msg("pgwire listening")

	var wg sync.WaitGroup
	defer wg.Wait()

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.Log.Error().Err(err).Msg("pgwire accept")
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handle(ctx, conn)
		}()
	}
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	be := pgproto3.NewBackend(conn, conn)

	msg, err := be.ReceiveStartupMessage()
	if err != nil {
		return
	}

	switch m := msg.(type) {
	case *pgproto3.StartupMessage:
		s.Log.Debug().
			Str("user", m.Parameters["user"]).
			Str("database", m.Parameters["database"]).
			Msg("pgwire connect")
	case *pgproto3.SSLRequest:
		if _, err := conn.Write([]byte{'N'}); err != nil {
			return
		}
		msg, err = be.ReceiveStartupMessage()
		if err != nil {
			return
		}
		if _, ok := msg.(*pgproto3.StartupMessage); !ok {
			return
		}
	default:
		return
	}

	be.Send(&pgproto3.AuthenticationOk{})
	be.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "16.0 (pyramid)"})
	be.Send(&pgproto3.ParameterStatus{Name: "server_encoding", Value: "UTF8"})
	be.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
	be.Send(&pgproto3.ParameterStatus{Name: "DateStyle", Value: "ISO, MDY"})
	be.Send(&pgproto3.ParameterStatus{Name: "TimeZone", Value: "UTC"})
	be.Send(&pgproto3.ParameterStatus{Name: "integer_datetimes", Value: "on"})
	be.Send(&pgproto3.ParameterStatus{Name: "standard_conforming_strings", Value: "on"})
	be.Send(&pgproto3.ParameterStatus{Name: "application_name", Value: "pyramid"})
	be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := be.Flush(); err != nil {
		return
	}

	for {
		msg, err := be.Receive()
		if err != nil {
			return
		}
		switch m := msg.(type) {
		case *pgproto3.Query:
			s.handleSimpleQuery(be, m.String)
		case *pgproto3.Parse:
			s.handleParse(be, m)
		case *pgproto3.Bind:
			s.bindStmt(be, m)
		case *pgproto3.Describe:
			s.describeStmt(be, m)
		case *pgproto3.Execute:
			s.executeStmt(be, m)
		case *pgproto3.Sync:
			be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
			_ = be.Flush()
		case *pgproto3.Terminate:
			return
		}
	}
}

// handleSimpleQuery handles a single SQL string with the simple query
// protocol. The string may contain multiple semicolon-separated statements.
func (s *Server) handleSimpleQuery(be *pgproto3.Backend, sql string) {
	if debugSQL {
		fmt.Println("PGSQL>>", sql)
	}
	stmts, err := pgparser.Parse(preprocessSQL(sql))
	if err != nil {
		sendError(be, "42601", "syntax error: "+err.Error())
		finish(be)
		return
	}
	if len(stmts) == 0 {
		be.Send(&pgproto3.EmptyQueryResponse{})
		finish(be)
		return
	}
	for _, stmt := range stmts {
		s.dispatch(be, stmt.AST)
	}
	finish(be)
}

// dispatch sends the wire messages for a single parsed statement. It does not
// call Flush itself; callers do it with finish().
func (s *Server) dispatch(be *pgproto3.Backend, stmt tree.Statement) {
	switch n := stmt.(type) {
	case *tree.Select:
		s.execSelect(be, n)
	case *tree.SelectClause:
		// plain select without ORDER/LIMIT
		s.execSelect(be, &tree.Select{Select: n})
	case *tree.ParenSelect:
		s.execSelect(be, n.Select)
	case *tree.ShowVar:
		s.execShowVar(be, n)
	case *tree.ShowClusterSetting:
		// respond with a one-row result showing the cluster setting as empty
		s.execScalarConst(be, "", Name)
	case *tree.SetVar:
		be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SET")})
	case *tree.SetClusterSetting:
		be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SET CLUSTER SETTING")})
	case *tree.SetTransaction:
		be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SET")})
	case *tree.BeginTransaction:
		be.Send(&pgproto3.CommandComplete{CommandTag: []byte("BEGIN")})
	case *tree.CommitTransaction:
		be.Send(&pgproto3.CommandComplete{CommandTag: []byte("COMMIT")})
	case *tree.RollbackTransaction:
		be.Send(&pgproto3.CommandComplete{CommandTag: []byte("ROLLBACK")})
	default:
		// best-effort: respond with generic ack so the session continues
		s.Log.Debug().Type("stmt", stmt).Msg("unsupported statement; acking")
		be.Send(&pgproto3.CommandComplete{CommandTag: []byte(tagFor(stmt))})
	}
}

// tagFor returns a CommandTag string for the unknown node. Defaults to "OK".
func tagFor(stmt tree.Statement) string {
	if t, ok := stmt.(interface{ StatementTag() string }); ok {
		return t.StatementTag()
	}
	return "OK"
}

// finish flushes and emits a ReadyForQuery frame (simple protocol).
func finish(be *pgproto3.Backend) {
	be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	_ = be.Flush()
}

func sendError(be *pgproto3.Backend, code, msg string) {
	be.Send(&pgproto3.ErrorResponse{
		Severity: "ERROR",
		Code:     code,
		Message:  msg,
	})
}

// extended-protocol stubs: we don't fully support prepared statements. We keep
// enough state so clients that try them (odbc, some drivers) at least see a
// benign error or a no-op. psql uses the simple protocol for catalog queries.

func (s *Server) handleParse(be *pgproto3.Backend, msg *pgproto3.Parse) {
	stmts, err := pgparser.Parse(preprocessSQL(msg.Query))
	if err != nil {
		sendError(be, "42601", "syntax error: "+err.Error())
		_ = be.Flush()
		return
	}
	// stash under prepared name, zero-or-one
	if len(stmts) > 0 {
		prepared[msg.Name] = stmts[0].AST
	}
	be.Send(&pgproto3.ParseComplete{})
	_ = be.Flush()
}

func (s *Server) describeStmt(be *pgproto3.Backend, msg *pgproto3.Describe) {
	// we don't expose real prepared descriptions; just signal NoData
	if msg.ObjectType == 'S' {
		// portal describe; send NoData to keep it simple
		be.Send(&pgproto3.NoData{})
		_ = be.Flush()
		return
	}
	// statement describe: ParameterDescription (none) + NoData
	be.Send(&pgproto3.ParameterDescription{ParameterOIDs: nil})
	be.Send(&pgproto3.NoData{})
	_ = be.Flush()
}

func (s *Server) bindStmt(be *pgproto3.Backend, msg *pgproto3.Bind) {
	stmt, ok := prepared[msg.PreparedStatement]
	if !ok {
		sendError(be, "26000", "prepared statement does not exist")
		_ = be.Flush()
		return
	}
	portals[msg.DestinationPortal] = &boundPortal{stmt: stmt}
	be.Send(&pgproto3.BindComplete{})
	_ = be.Flush()
}

func (s *Server) executeStmt(be *pgproto3.Backend, msg *pgproto3.Execute) {
	p, ok := portals[msg.Portal]
	if !ok {
		sendError(be, "34000", "portal does not exist")
		_ = be.Flush()
		return
	}
	s.dispatch(be, p.stmt)
	be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	_ = be.Flush()
	delete(portals, msg.Portal)
}

// prepared / portals are stored per-process. Since pgwire sessions are short
// lived and the parse/bind names are session-scoped, collisions are unlikely.
// This is best-effort scaffolding for tools that probe with extended protocol.
var (
	prepared = map[string]tree.Statement{}
	portals  = map[string]*boundPortal{}
)

type boundPortal struct {
	stmt tree.Statement
}
