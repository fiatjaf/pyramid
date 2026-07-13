package pg

import (
	"context"
	"encoding/hex"
	"fmt"
	"iter"
	"net"
	"strconv"
	"strings"
	"sync"

	"fiatjaf.com/nostr"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/pganalyze/pg_query_go/v6"
	"github.com/rs/zerolog"
)

type EventStore interface {
	QueryEvents(filter nostr.Filter, maxLimit int) iter.Seq[nostr.Event]
	CountEvents(filter nostr.Filter) (uint32, error)
}

type Server struct {
	Log   zerolog.Logger
	Store EventStore
	Host  string
	Port  int
}

func (s *Server) Start(ctx context.Context) error {
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("pgwire listen on %s: %w", addr, err)
	}
	s.Log.Info().Str("addr", addr).Msg("pgwire listening")

	var wg sync.WaitGroup
	defer wg.Wait()

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
		s.Log.Debug().Str("user", m.Parameters["user"]).Str("database", m.Parameters["database"]).Msg("pgwire connect")
	case *pgproto3.SSLRequest:
		conn.Write([]byte{'N'})
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
	be.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "16.0"})
	be.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
	be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	be.Flush()

	for {
		msg, err := be.Receive()
		if err != nil {
			return
		}

		switch m := msg.(type) {
		case *pgproto3.Query:
			s.handleQuery(be, m.String)
		case *pgproto3.Terminate:
			return
		}
	}
}

type columnInfo struct {
	Name string
	OID  uint32
}

var eventColumns = []columnInfo{
	{Name: "id", OID: 25},
	{Name: "pubkey", OID: 25},
	{Name: "kind", OID: 23},
	{Name: "created_at", OID: 20},
	{Name: "content", OID: 25},
	{Name: "tags", OID: 25},
	{Name: "sig", OID: 25},
}

func formatEventRow(evt nostr.Event) [][]byte {
	tagsJSON := "["
	for i, tag := range evt.Tags {
		if i > 0 {
			tagsJSON += ","
		}
		tagsJSON += "["
		for j, v := range tag {
			if j > 0 {
				tagsJSON += ","
			}
			tagsJSON += strconv.Quote(v)
		}
		tagsJSON += "]"
	}
	tagsJSON += "]"

	return [][]byte{
		[]byte(evt.ID.Hex()),
		[]byte(evt.PubKey.Hex()),
		[]byte(strconv.Itoa(int(evt.Kind))),
		[]byte(strconv.FormatInt(int64(evt.CreatedAt), 10)),
		[]byte(evt.Content),
		[]byte(tagsJSON),
		[]byte(hex.EncodeToString(evt.Sig[:])),
	}
}

func rowDescFromColumns(cols []columnInfo) *pgproto3.RowDescription {
	fd := make([]pgproto3.FieldDescription, len(cols))
	for i, c := range cols {
		fd[i] = pgproto3.FieldDescription{
			Name:                 []byte(c.Name),
			TableOID:             0,
			TableAttributeNumber: 0,
			DataTypeOID:          c.OID,
			DataTypeSize:         -1,
			TypeModifier:         -1,
			Format:               0,
		}
	}
	return &pgproto3.RowDescription{Fields: fd}
}

func (s *Server) handleQuery(be *pgproto3.Backend, sql string) {
	defer be.Flush()

	if strings.TrimSpace(sql) == "" {
		be.Send(&pgproto3.EmptyQueryResponse{})
		be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		return
	}

	tree, err := pg_query.Parse(sql)
	if err != nil {
		be.Send(&pgproto3.ErrorResponse{
			Severity: "ERROR",
			Code:     "42601",
			Message:  fmt.Sprintf("parse error: %v", err),
		})
		be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		return
	}

	if len(tree.Stmts) == 0 {
		be.Send(&pgproto3.EmptyQueryResponse{})
		be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		return
	}

	stmt := tree.Stmts[0].GetStmt()
	if sel := stmt.GetSelectStmt(); sel != nil {
		s.execSelect(be, sel)
	} else {
		be.Send(&pgproto3.ErrorResponse{
			Severity: "ERROR",
			Code:     "42601",
			Message:  "only SELECT is supported",
		})
		be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	}
}

type selectInfo struct {
	Columns []columnInfo
	Table   string
	Filter  nostr.Filter
	Limit   int
	IsCount bool
}

func (s *Server) execSelect(be *pgproto3.Backend, sel *pg_query.SelectStmt) {
	info := parseSelect(sel)
	if info.IsCount {
		s.execCount(be, info)
	} else {
		s.execRows(be, info)
	}
}

func (s *Server) execCount(be *pgproto3.Backend, info *selectInfo) {
	be.Send(rowDescFromColumns([]columnInfo{{Name: "count", OID: 20}}))

	count, err := s.Store.CountEvents(info.Filter)
	if err != nil {
		count = 0
	}

	be.Send(&pgproto3.DataRow{Values: [][]byte{[]byte(strconv.FormatUint(uint64(count), 10))}})
	be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
	be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
}

func (s *Server) execRows(be *pgproto3.Backend, info *selectInfo) {
	limit := info.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	be.Send(rowDescFromColumns(info.Columns))

	n := 0
	for evt := range s.Store.QueryEvents(info.Filter, limit) {
		row := formatEventRow(evt)
		vals := make([][]byte, len(info.Columns))
		for i, col := range info.Columns {
			switch col.Name {
			case "id":
				vals[i] = row[0]
			case "pubkey":
				vals[i] = row[1]
			case "kind":
				vals[i] = row[2]
			case "created_at":
				vals[i] = row[3]
			case "content":
				vals[i] = row[4]
			case "tags":
				vals[i] = row[5]
			case "sig":
				vals[i] = row[6]
			default:
				vals[i] = row[4]
			}
		}
		be.Send(&pgproto3.DataRow{Values: vals})
		n++
		if n >= limit {
			break
		}
	}

	if n == 0 {
		be.Send(&pgproto3.DataRow{Values: make([][]byte, len(info.Columns))})
		n++
	}

	be.Send(&pgproto3.CommandComplete{CommandTag: []byte(fmt.Sprintf("SELECT %d", n))})
	be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
}

func parseSelect(sel *pg_query.SelectStmt) *selectInfo {
	info := &selectInfo{
		Columns: eventColumns,
		Filter:  nostr.Filter{},
		Limit:   100,
	}

	for _, item := range sel.GetFromClause() {
		if rv := item.GetRangeVar(); rv != nil {
			info.Table = rv.Relname
		}
	}

	cols := []columnInfo{}
	countStar := false
	for _, item := range sel.GetTargetList() {
		if rt := item.GetResTarget(); rt != nil {
			name := rt.GetName()
			if strings.ToLower(name) == "count" {
				countStar = true
			}
			if val := rt.GetVal(); val != nil {
				if cr := val.GetColumnRef(); cr != nil {
					for _, f := range cr.GetFields() {
						if f.GetAStar() != nil {
							cols = append(cols, eventColumns...)
						} else if s := f.GetString_(); s != nil {
							cols = append(cols, columnInfo{Name: s.GetSval(), OID: 25})
						}
					}
				}
				if ac := val.GetAConst(); ac != nil {
					cols = append(cols, columnInfo{Name: "?column?", OID: 25})
				}
				if fc := val.GetFuncCall(); fc != nil {
					cols = append(cols, columnInfo{Name: name, OID: 25})
				}
			}
		}
	}

	if countStar && len(cols) == 0 {
		info.IsCount = true
		return info
	}
	if len(cols) > 0 {
		info.Columns = cols
	}

	if wc := sel.GetWhereClause(); wc != nil {
		parseWhere(wc, info)
	}

	if lc := sel.GetLimitCount(); lc != nil {
		if ac := lc.GetAConst(); ac != nil {
			if i := ac.GetIval(); i != nil {
				info.Limit = int(i.GetIval())
			}
		}
	}

	if info.Table != "" && !strings.EqualFold(info.Table, "events") {
		info.Limit = 0
	}

	return info
}

func parseWhere(node *pg_query.Node, info *selectInfo) {
	if node == nil {
		return
	}

	if be := node.GetBoolExpr(); be != nil {
		for _, arg := range be.GetArgs() {
			parseWhere(arg, info)
		}
		return
	}

	if ae := node.GetAExpr(); ae != nil {
		left := ae.GetLexpr()
		right := ae.GetRexpr()

		colName := extractColumnName(left)
		strVal := extractStringValue(right)

		if colName == "" || strVal == "" {
			return
		}

		switch strings.ToLower(colName) {
		case "kind":
			if k, err := strconv.Atoi(strVal); err == nil {
				info.Filter.Kinds = []nostr.Kind{nostr.Kind(k)}
			}
		case "pubkey":
			if pk, err := nostr.PubKeyFromHex(strVal); err == nil {
				info.Filter.Authors = []nostr.PubKey{pk}
			}
		case "id":
			if id, err := nostr.IDFromHex(strVal); err == nil {
				info.Filter.IDs = []nostr.ID{id}
			}
		case "content":
			info.Filter.Search = strVal
		case "since":
			if ts, err := strconv.ParseInt(strVal, 10, 64); err == nil {
				info.Filter.Since = nostr.Timestamp(ts)
			}
		case "until":
			if ts, err := strconv.ParseInt(strVal, 10, 64); err == nil {
				info.Filter.Until = nostr.Timestamp(ts)
			}
		case "limit":
			if l, err := strconv.Atoi(strVal); err == nil {
				info.Limit = l
			}
		}
		return
	}
}

func extractColumnName(node *pg_query.Node) string {
	if node == nil {
		return ""
	}
	if cr := node.GetColumnRef(); cr != nil {
		for _, f := range cr.GetFields() {
			if s := f.GetString_(); s != nil {
				return s.GetSval()
			}
		}
	}
	return ""
}

func extractStringValue(node *pg_query.Node) string {
	if node == nil {
		return ""
	}
	if ac := node.GetAConst(); ac != nil {
		if s := ac.GetSval(); s != nil {
			return s.GetSval()
		}
		if i := ac.GetIval(); i != nil {
			return strconv.Itoa(int(i.GetIval()))
		}
	}
	return ""
}
