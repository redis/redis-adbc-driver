// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type sequenceOptions struct {
	NoMin, NoMax                      bool
	Increment, Min, Max, Start, Cache *int64
	Cycle                             *bool
	Restart                           *int64
	RestartDefault                    bool
	Owned                             *TableName
	Column                            string
	Kind                              Kind
}
type SequenceStmt struct {
	Action      string
	Name        TableName
	Options     sequenceOptions
	IfExists    bool
	IfNotExists bool
	Cascade     bool
}

func (*SequenceStmt) stmtNode() {}

type sequenceBlock struct {
	Next, Step, Remaining, Epoch int64
	Token                        string
}

type sequenceMeta struct {
	ResetEpoch, ResetLast                   int64
	ResetCalled                             bool
	snapshotToken                           string
	OwnerOID                                int64
	OwnerField                              string
	Epoch                                   int64
	Schema, Name, ID                        string
	Increment, Min, Max, Start, Cache, Last int64
	Called, Cycle                           bool
	Owned                                   TableName
	Column                                  string
	Kind                                    Kind
}

func sequenceKey(schema, name string) string {
	return metaPrefix + "sql-sequence:" + tableKeySuffix(schema, name)
}
func sequencesKey(schema string) string { return metaPrefix + "sql-sequences:" + escapeKeyPart(schema) }
func (p *parser) sequenceNumber() (int64, error) {
	sign := ""
	if p.acceptOp("-") {
		sign = "-"
	} else {
		p.acceptOp("+")
	}
	t := p.next()
	v, err := strconv.ParseInt(sign+t.text, 10, 64)
	if err != nil {
		return 0, syntaxErr("invalid sequence value %q", sign+t.text)
	}
	return v, nil
}
func (p *parser) parseSequenceOptions() (sequenceOptions, error) {
	var o sequenceOptions
	for {
		var dst **int64
		switch {
		case p.acceptKeyword("AS"):
			spec, err := p.parseTypeSpec()
			if err != nil {
				return o, err
			}
			t, err := colTypeFromSQL(spec)
			if err != nil {
				return o, err
			}
			if !t.Kind.isInteger() {
				return o, stateErrorf(stateInvalidParameter, "sequence type must be smallint, integer, or bigint")
			}
			o.Kind = t.Kind
			continue
		case p.acceptKeyword("INCREMENT"):
			p.acceptKeyword("BY")
			dst = &o.Increment
		case p.acceptKeyword("MINVALUE"):
			dst = &o.Min
		case p.acceptKeyword("MAXVALUE"):
			dst = &o.Max
		case p.acceptKeyword("START"):
			p.acceptKeyword("WITH")
			dst = &o.Start
		case p.acceptKeyword("CACHE"):
			dst = &o.Cache
		case p.acceptKeyword("RESTART"):
			p.acceptKeyword("WITH")
			if p.peek().kind != tokNumber && !p.isOp("-") && !p.isOp("+") {
				o.RestartDefault = true
				continue
			}
			dst = &o.Restart
		case p.acceptKeyword("CYCLE"):
			v := true
			o.Cycle = &v
			continue
		case p.acceptKeyword("NO", "CYCLE"):
			v := false
			o.Cycle = &v
			continue
		case p.acceptKeyword("NO", "MINVALUE"):
			o.NoMin = true
			continue
		case p.acceptKeyword("NO", "MAXVALUE"):
			o.NoMax = true
			continue
		case p.acceptKeyword("OWNED", "BY"):
			t := TableName{}
			if p.acceptKeyword("NONE") {
				o.Owned = &t
				continue
			}
			n, err := p.parseTableName()
			if err != nil {
				return o, err
			}
			if n.Schema == "" {
				return o, syntaxErr("OWNED BY needs table.column")
			}
			o.Column = n.Name
			t = TableName{Schema: n.Catalog, Name: n.Schema}
			o.Owned = &t
			continue
		default:
			return o, nil
		}
		if *dst != nil {
			return o, syntaxErr("conflicting or redundant sequence options")
		}
		n, err := p.sequenceNumber()
		if err != nil {
			return o, err
		}
		*dst = &n
	}
}
func (p *parser) parseSequence(action string) (Stmt, error) {
	st := &SequenceStmt{Action: action}
	if action == "CREATE" {
		st.IfNotExists = p.acceptKeyword("IF", "NOT", "EXISTS")
	} else {
		st.IfExists = p.acceptKeyword("IF", "EXISTS")
	}
	var err error
	if st.Name, err = p.parseTableName(); err != nil {
		return nil, err
	}
	if action == "DROP" {
		st.Cascade = p.parseDropBehavior()
		return st, nil
	}
	if action == "ALTER" && p.isKeyword("RENAME") {
		return nil, stateErrorf(stateFeatureNotSupported, "ALTER SEQUENCE RENAME is not supported")
	}
	st.Options, err = p.parseSequenceOptions()
	return st, err
}
func sequenceBounds(k Kind) (int64, int64) {
	switch k {
	case KindInt16:
		return math.MinInt16, math.MaxInt16
	case KindInt32:
		return math.MinInt32, math.MaxInt32
	}
	return math.MinInt64, math.MaxInt64
}
func (m *sequenceMeta) apply(o sequenceOptions, create bool) error {
	if create {
		m.Kind = KindInt64
		m.Increment = 1
		m.Cache = 1
	}
	if o.Kind != KindNull {
		m.Kind = o.Kind
	}
	if o.Increment != nil {
		m.Increment = *o.Increment
	}
	if m.Increment == 0 {
		return stateErrorf(stateInvalidParameter, "INCREMENT must not be zero")
	}
	low, high := sequenceBounds(m.Kind)
	if create {
		if m.Increment > 0 {
			m.Min, m.Max = 1, high
		} else {
			m.Min, m.Max = low, -1
		}
	}
	if o.NoMin {
		if m.Increment > 0 {
			m.Min = 1
		} else {
			m.Min = low
		}
	}
	if o.Min != nil {
		m.Min = *o.Min
	}
	if o.NoMax {
		if m.Increment > 0 {
			m.Max = high
		} else {
			m.Max = -1
		}
	}
	if o.Max != nil {
		m.Max = *o.Max
	}
	if create {
		if m.Increment > 0 {
			m.Start = m.Min
		} else {
			m.Start = m.Max
		}
	}
	if o.Start != nil {
		m.Start = *o.Start
	}
	if o.Cache != nil {
		m.Cache = *o.Cache
	}
	if o.Cycle != nil {
		m.Cycle = *o.Cycle
	}
	if m.Min < low || m.Max > high || m.Min >= m.Max || m.Start < m.Min || m.Start > m.Max || m.Cache < 1 {
		return stateErrorf(stateInvalidParameter, "invalid sequence bounds, start, or cache size")
	}
	if create {
		m.Last = m.Start
		m.Called = false
		m.ResetLast = m.Start
	}
	if o.RestartDefault {
		m.Last, m.Called = m.Start, false
		m.ResetEpoch++
		m.ResetLast, m.ResetCalled = m.Last, m.Called
	}
	if o.Restart != nil {
		if *o.Restart < m.Min || *o.Restart > m.Max {
			return stateErrorf(stateInvalidParameter, "RESTART value out of bounds")
		}
		m.Last, m.Called = *o.Restart, false
		m.ResetEpoch++
		m.ResetLast, m.ResetCalled = m.Last, m.Called
	}
	if o.Owned != nil {
		m.OwnerOID, m.OwnerField = 0, ""
		m.Owned = *o.Owned
		m.Column = o.Column
	}
	return nil
}
func (s *store) getSequence(ctx context.Context, schema, name string) (*sequenceMeta, error) {
	raw, err := s.redis(ctx).Get(ctx, sequenceKey(schema, name)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, stateErrorf(stateUndefinedTable, "sequence %q does not exist", qualifiedName(schema, name))
	}
	if err != nil {
		return nil, wrapRedis(err, "read sequence")
	}
	var m sequenceMeta
	if err = json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if s.snapshots {
		state, _, err := s.readSnapshotSequenceValue(ctx, &m)
		if err != nil {
			return nil, err
		}
		overlaySnapshotSequenceValue(&m, state)
	}
	return &m, nil
}
func (e *executor) runSequence(ctx context.Context, st *SequenceStmt) error {
	schema, name, err := e.resolveCreate(ctx, st.Name, false)
	if err != nil {
		return err
	}
	if err = e.store.checkWritableSchema(schema); err != nil {
		return err
	}
	key := sequenceKey(schema, name)
	if st.Action == "CREATE" {
		for _, check := range []func(context.Context, string, string) (bool, error){e.store.tableExists, e.store.viewExists} {
			exists, err := check(ctx, schema, name)
			if err != nil {
				return err
			}
			if exists {
				if st.IfNotExists {
					return nil
				}
				return stateErrorf(stateDuplicateTable, "relation %q already exists", name)
			}
		}
		token, err := randomUUID()
		if err != nil {
			return err
		}
		m := &sequenceMeta{Schema: schema, Name: name, ID: token.S}
		if err = m.apply(st.Options, true); err != nil {
			return err
		}
		if st.Options.Owned != nil {
			if err = e.validateSequenceOwner(ctx, m); err != nil {
				return err
			}
		}
		raw, _ := json.Marshal(m)
		return e.store.redis(ctx).Watch(ctx, func(tx *goredis.Tx) error {
			n, err := tx.Exists(ctx, key, metaKey(schema, name), viewKey(schema, name), sqlIndexKey(schema, name)).Result()
			if err != nil {
				return err
			}
			if n > 0 {
				if st.IfNotExists {
					return nil
				}
				return stateErrorf(stateDuplicateTable, "relation %q already exists", name)
			}
			_, err = tx.TxPipelined(ctx, func(p goredis.Pipeliner) error {
				if e.store.snapshots {
					state, _ := json.Marshal(snapshotSequenceValue{ID: m.ID, Last: m.Last, Called: m.Called, ResetEpoch: m.ResetEpoch, ConfigEpoch: m.Epoch})
					p.Set(ctx, snapshotSequenceKey(m.ID), state, 0)
				}
				p.Set(ctx, key, raw, 0)
				p.SAdd(ctx, sequencesKey(schema), name)
				p.SAdd(ctx, schemasKey, schema)
				return nil
			})
			return err
		}, key, metaKey(schema, name), viewKey(schema, name), sqlIndexKey(schema, name))
	}
	return e.mutateSequence(ctx, schema, name, func(m *sequenceMeta) error {
		if st.Action == "DROP" {
			if m.Owned.Name != "" && !st.Cascade {
				return stateErrorf("2BP01", "cannot drop owned sequence %q without CASCADE", name)
			}
			return nil
		}
		m.Epoch++
		if err := m.apply(st.Options, false); err != nil {
			return err
		}
		if st.Options.Owned != nil {
			return e.validateSequenceOwner(ctx, m)
		}
		return nil
	}, st.Action == "DROP", st.IfExists)
}
func (e *executor) mutateSequence(ctx context.Context, schema, name string, fn func(*sequenceMeta) error, drop, ifExists bool) error {
	key := sequenceKey(schema, name)
	for attempt := 0; attempt < 100; attempt++ {
		err := e.store.redis(ctx).Watch(ctx, func(tx *goredis.Tx) error {
			raw, err := tx.Get(ctx, key).Bytes()
			if errors.Is(err, goredis.Nil) {
				if ifExists {
					return nil
				}
				return stateErrorf(stateUndefinedTable, "sequence %q does not exist", qualifiedName(schema, name))
			}
			if err != nil {
				return err
			}
			var m sequenceMeta
			if err = json.Unmarshal(raw, &m); err != nil {
				return err
			}
			if err = fn(&m); err != nil {
				return err
			}
			raw, err = json.Marshal(&m)
			if err != nil {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(p goredis.Pipeliner) error {
				if drop {
					if e.store.snapshots {
						p.Del(ctx, snapshotSequenceKey(m.ID))
					}
					p.Del(ctx, key)
					p.SRem(ctx, sequencesKey(schema), name)
				} else {
					p.Set(ctx, key, raw, 0)
				}
				return nil
			})
			return err
		}, key)
		if errors.Is(err, goredis.TxFailedErr) {
			continue
		}
		return err
	}
	return errorf(adbc.StatusIO, "sequence is being changed concurrently; try again")
}
func (e *executor) sequenceName(s string) (string, string, error) {
	toks, err := lex(s)
	if err != nil {
		return "", "", err
	}
	p := &parser{src: s, toks: toks}
	n, err := p.parseTableName()
	if err != nil {
		return "", "", err
	}
	if p.peek().kind != tokEOF {
		return "", "", syntaxErr("invalid sequence name")
	}
	return e.resolveTable(n)
}
func sequenceFunction(name string) bool {
	return name == "NEXTVAL" || name == "CURRVAL" || name == "SETVAL" || name == "LASTVAL"
}
func (env *evalEnv) evalSequence(f *Func) (Value, bool, error) {
	if !sequenceFunction(f.Name) {
		return Value{}, false, nil
	}
	if scope := snapshotOf(env.ctx); scope != nil && scope.sharedRead {
		return Value{}, true, sharedSnapshotUnsupported()
	}
	e := env.exec
	if e == nil || e.store == nil {
		return Value{}, true, stateErrorf(stateFeatureNotSupported, "sequence functions need a database connection")
	}
	if e.sess == nil {
		e.sess = newSession(e.schema)
	}
	sess := e.sess
	if sess.sequenceValues == nil {
		sess.sequenceValues = map[string]int64{}
	}
	if f.Name == "LASTVAL" {
		if sess.lastSequence == "" {
			return Value{}, true, stateErrorf("55000", "lastval is not yet defined in this session")
		}
		m, err := e.store.getSequence(env.ctx, sess.lastSequenceName.Schema, sess.lastSequenceName.Name)
		if err != nil {
			return Value{}, true, err
		}
		if m.ID != sess.lastSequence {
			return Value{}, true, stateErrorf("55000", "lastval sequence no longer exists")
		}
		v, ok := sess.sequenceValues[sess.lastSequence]
		if !ok {
			return Value{}, true, stateErrorf("55000", "lastval is not yet defined")
		}
		return intValue(typeInt64, v), true, nil
	}
	args := make([]Value, len(f.Args))
	for i, a := range f.Args {
		v, err := env.eval(a)
		if err != nil {
			return Value{}, true, err
		}
		if v.Null {
			return nullValue(typeInt64), true, nil
		}
		args[i] = v
	}
	if len(args) == 0 {
		return Value{}, true, stateErrorf(stateInvalidParameter, "sequence name is required")
	}
	schema, name, err := e.sequenceName(args[0].Text())
	if err != nil {
		return Value{}, true, err
	}
	m, err := e.store.getSequence(env.ctx, schema, name)
	if err != nil {
		return Value{}, true, err
	}
	if f.Name == "CURRVAL" {
		v, ok := sess.sequenceValues[m.ID]
		if !ok {
			return Value{}, true, stateErrorf("55000", "currval of sequence %q is not yet defined in this session", name)
		}
		return intValue(typeInt64, v), true, nil
	}
	if sess.sequenceBlocks == nil {
		sess.sequenceBlocks = map[string]sequenceBlock{}
	}
	if f.Name == "NEXTVAL" {
		if block, ok := sess.sequenceBlocks[m.ID]; ok && block.Remaining > 0 && block.Epoch == m.Epoch && block.Token == m.snapshotToken {
			result := block.Next
			block.Remaining--
			if block.Remaining > 0 {
				block.Next += block.Step
			}
			sess.sequenceBlocks[m.ID] = block
			sess.sequenceValues[m.ID] = result
			sess.lastSequence = m.ID
			sess.lastSequenceName = TableName{Schema: schema, Name: name}
			return intValue(typeInt64, result), true, nil
		}
	}
	var reserved sequenceBlock
	var result int64
	id := ""
	called := true
	mutate := e.mutateSequence
	if e.store.snapshots {
		mutate = e.mutateSnapshotSequenceValue
	}
	err = mutate(env.ctx, schema, name, func(m *sequenceMeta) error {
		id = m.ID
		if f.Name == "SETVAL" {
			v, err := Coerce(args[1], typeInt64)
			if err != nil {
				return err
			}
			result = v.I
			if result < m.Min || result > m.Max {
				return stateErrorf(stateNumericRange, "setval value is out of bounds for sequence %q", name)
			}
			if len(args) > 2 {
				b, err := Coerce(args[2], typeBool)
				if err != nil {
					return err
				}
				called = b.I != 0
			}
			m.Epoch++
			m.Last, m.Called = result, called
			return nil
		}
		result = m.Last
		if m.Called {
			next := new(big.Int).Add(big.NewInt(m.Last), big.NewInt(m.Increment))
			if !next.IsInt64() || next.Cmp(big.NewInt(m.Max)) > 0 || next.Cmp(big.NewInt(m.Min)) < 0 {
				if !m.Cycle {
					return stateErrorf("2200H", "nextval reached limit of sequence %q", name)
				}
				if m.Increment > 0 {
					result = m.Min
				} else {
					result = m.Max
				}
			} else {
				result = next.Int64()
			}
		}
		// Reserve at most CACHE values, stopping at the current bound. Cached
		// values belong to this connection and may become gaps when it closes.
		distance := new(big.Int)
		if m.Increment > 0 {
			distance.Sub(big.NewInt(m.Max), big.NewInt(result))
		} else {
			distance.Sub(big.NewInt(result), big.NewInt(m.Min))
		}
		step := new(big.Int).Abs(big.NewInt(m.Increment))
		available := new(big.Int).Quo(distance, step)
		available.Add(available, big.NewInt(1))
		count := m.Cache
		if available.Cmp(big.NewInt(count)) < 0 {
			count = available.Int64()
		}
		end := new(big.Int).Mul(big.NewInt(count-1), big.NewInt(m.Increment))
		end.Add(end, big.NewInt(result))
		reserved = sequenceBlock{Step: m.Increment, Remaining: count - 1, Epoch: m.Epoch, Token: m.snapshotToken}
		if count > 1 {
			reserved.Next = result + m.Increment
		}
		m.Last, m.Called = end.Int64(), true
		return nil
	}, false, false)
	if err != nil {
		return Value{}, true, err
	}
	if f.Name == "NEXTVAL" {
		sess.sequenceBlocks[id] = reserved
	} else {
		delete(sess.sequenceBlocks, id)
	}
	if called {
		sess.sequenceValues[id] = result
		if f.Name == "NEXTVAL" {
			sess.lastSequence = id
			sess.lastSequenceName = TableName{Schema: schema, Name: name}
		}
	}
	return intValue(typeInt64, result), true, nil
}
func sequenceDefault(schema, name string) string {
	if isTempSchema(schema) {
		schema = tempAlias
	}
	return "nextval('" + strings.ReplaceAll(qualifiedName(schema, name), "'", "''") + "')"
}

func (p *parser) parseIdentity() (string, sequenceOptions, error) {
	mode := "ALWAYS"
	if !p.acceptKeyword("ALWAYS") {
		if err := p.expectKeyword("BY", "DEFAULT"); err != nil {
			return "", sequenceOptions{}, err
		}
		mode = "BY DEFAULT"
	}
	if err := p.expectKeyword("AS", "IDENTITY"); err != nil {
		return "", sequenceOptions{}, err
	}
	var o sequenceOptions
	var err error
	if p.acceptOp("(") {
		if o, err = p.parseSequenceOptions(); err != nil {
			return "", o, err
		}
		if err = p.expectOp(")"); err != nil {
			return "", o, err
		}
	}
	return mode, o, nil
}

func (e *executor) autoColumns(ctx context.Context, schema, table string, defs []ColumnDef) ([]ColumnDef, []TableName, error) {
	out := append([]ColumnDef(nil), defs...)
	var created []TableName
	for i := range out {
		c := &out[i]
		if !c.Serial && c.Identity == "" {
			continue
		}
		if !c.Type.Kind.isInteger() {
			return out, created, stateErrorf(stateInvalidParameter, "identity column must be smallint, integer, or bigint")
		}
		if c.Default != nil {
			return out, created, stateErrorf(stateInvalidParameter, "identity or serial column cannot have another default")
		}
		base := table + "_" + c.Name + "_seq"
		name := base
		for suffix := 0; ; suffix++ {
			n, err := e.store.redis(ctx).Exists(ctx, sequenceKey(schema, name), metaKey(schema, name), viewKey(schema, name)).Result()
			if err != nil {
				return out, created, err
			}
			if n == 0 {
				break
			}
			name = base + strconv.Itoa(suffix+1)
		}
		o := c.SequenceOptions
		o.Kind = c.Type.Kind
		createSchema := schema
		if isTempSchema(schema) {
			createSchema = tempAlias
		}
		if err := e.runSequence(ctx, &SequenceStmt{Action: "CREATE", Name: TableName{Schema: createSchema, Name: name}, Options: o}); err != nil {
			return out, created, err
		}
		seq := TableName{Schema: schema, Name: name}
		created = append(created, seq)
		if err := e.mutateSequence(ctx, schema, name, func(m *sequenceMeta) error {
			m.Owned = TableName{Schema: schema, Name: table}
			m.Column = c.Name
			return nil
		}, false, false); err != nil {
			return out, created, err
		}
		c.DefaultText = sequenceDefault(schema, name)
		x, err := parseExprText(c.DefaultText)
		if err != nil {
			return out, created, err
		}
		c.Default = x
		c.NotNull = true
		c.Sequence = seq
	}
	return out, created, nil
}
func identityValueError(name string) error {
	return stateErrorf("428C9", "cannot insert a non-DEFAULT value into column %q", name)
}
func (s *store) ownedSequences(ctx context.Context, schema, table string, ids ...int64) ([]*sequenceMeta, error) {
	names, err := s.redis(ctx).SMembers(ctx, sequencesKey(schema)).Result()
	if err != nil {
		return nil, err
	}
	var out []*sequenceMeta
	for _, name := range names {
		m, err := s.getSequence(ctx, schema, name)
		if err != nil {
			return nil, err
		}
		if (m.Owned.Schema == schema && m.Owned.Name == table) || (len(ids) > 0 && m.OwnerOID != 0 && m.OwnerOID == ids[0]) {
			out = append(out, m)
		}
	}
	return out, nil
}
func (s *store) dropOwnedSequences(ctx context.Context, schema, table string, ids ...int64) error {
	seqs, err := s.ownedSequences(ctx, schema, table, ids...)
	if err != nil {
		return err
	}
	e := &executor{store: s}
	for _, m := range seqs {
		if err = e.mutateSequence(ctx, m.Schema, m.Name, func(cur *sequenceMeta) error {
			if cur.ID != m.ID {
				return stateErrorf("55000", "owned sequence changed concurrently")
			}
			return nil
		}, true, true); err != nil {
			return err
		}
	}
	return nil
}
func (e *executor) restartOwnedSequences(ctx context.Context, metas []*tableMeta) error {
	for _, m := range metas {
		seqs, err := e.store.ownedSequences(ctx, m.Schema, m.Name, tableOID(m))
		if err != nil {
			return err
		}
		for _, seq := range seqs {
			if err = e.mutateSequence(ctx, seq.Schema, seq.Name, func(cur *sequenceMeta) error {
				cur.Epoch++
				cur.ResetEpoch++
				cur.Last, cur.Called = cur.Start, false
				cur.ResetLast, cur.ResetCalled = cur.Last, cur.Called
				return nil
			}, false, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *executor) sequenceRows(ctx context.Context, schema string) ([][]Value, error) {
	names, err := e.store.redis(ctx).SMembers(ctx, sequencesKey(schema)).Result()
	if err != nil {
		return nil, err
	}
	var rows [][]Value
	for _, name := range names {
		m, err := e.store.getSequence(ctx, schema, name)
		if err != nil {
			return nil, err
		}
		rows = append(rows, []Value{str(catalogName), str(displaySchema(schema)), str(name), str(strings.ToLower(ColType{Kind: m.Kind}.SQLName())), intValue(typeInt64, map[Kind]int64{KindInt16: 16, KindInt32: 32, KindInt64: 64}[m.Kind]), intValue(typeInt64, 2), intValue(typeInt64, 0), str(strconv.FormatInt(m.Start, 10)), str(strconv.FormatInt(m.Min, 10)), str(strconv.FormatInt(m.Max, 10)), str(strconv.FormatInt(m.Increment, 10)), yesNo(m.Cycle)})
	}
	return rows, nil
}

func (e *executor) alterIdentity(ctx context.Context, m *tableMeta, cmd AlterCmd) error {
	i, ok := m.resolve(cmd.Column)
	if !ok {
		return stateErrorf(stateUndefinedColumn, "column %q does not exist", cmd.Column)
	}
	col := m.Columns[i]
	switch cmd.Action {
	case AlterAddIdentity:
		if col.Identity != "" {
			return stateErrorf("55000", "column %q is already an identity column", col.Name)
		}
		if col.Nullable {
			return stateErrorf("55000", "column %q must be declared NOT NULL before identity can be added", col.Name)
		}
		if col.Default != "" {
			return stateErrorf("55000", "column %q already has a default value", col.Name)
		}
		defs, created, err := e.autoColumns(ctx, m.Schema, m.Name, []ColumnDef{{Name: col.Name, Type: col.Type, Identity: cmd.Def.Identity, SequenceOptions: cmd.Def.SequenceOptions}})
		if err != nil {
			return err
		}
		d := defs[0]
		err = e.store.updateTableTx(ctx, m.Schema, m.Name, false, func(cur *tableMeta, _ int64) error {
			j, ok := cur.resolve(col.Name)
			if !ok || cur.Columns[j].Identity != "" || cur.Columns[j].Default != "" {
				return stateErrorf("55000", "identity column changed concurrently")
			}
			cur.Columns[j].Identity = d.Identity
			cur.Columns[j].Sequence = d.Sequence
			cur.Columns[j].Default = d.DefaultText
			return nil
		}, nil)
		if err != nil {
			for _, n := range created {
				_ = e.mutateSequence(context.WithoutCancel(ctx), n.Schema, n.Name, func(*sequenceMeta) error { return nil }, true, true)
			}
		}
		return err
	case AlterDropIdentity:
		if col.Identity == "" {
			if cmd.IfColumnExists {
				return nil
			}
			return stateErrorf("55000", "column %q is not an identity column", col.Name)
		}
		if err := e.store.updateTableTx(ctx, m.Schema, m.Name, false, func(cur *tableMeta, _ int64) error {
			j, ok := cur.resolve(col.Name)
			if !ok {
				return stateErrorf(stateUndefinedColumn, "column no longer exists")
			}
			cur.Columns[j].Identity = ""
			cur.Columns[j].Sequence = TableName{}
			cur.Columns[j].Default = ""
			return nil
		}, nil); err != nil {
			return err
		}
		return e.mutateSequence(ctx, col.Sequence.Schema, col.Sequence.Name, func(*sequenceMeta) error { return nil }, true, true)
	case AlterSetIdentity:
		if col.Identity == "" {
			return stateErrorf("55000", "column %q is not an identity column", col.Name)
		}
		if cmd.Def.Identity != "" {
			return e.store.updateTableTx(ctx, m.Schema, m.Name, false, func(cur *tableMeta, _ int64) error {
				j, ok := cur.resolve(col.Name)
				if !ok {
					return stateErrorf(stateUndefinedColumn, "column no longer exists")
				}
				cur.Columns[j].Identity = cmd.Def.Identity
				return nil
			}, nil)
		}
		return e.mutateSequence(ctx, col.Sequence.Schema, col.Sequence.Name, func(seq *sequenceMeta) error { seq.Epoch++; return seq.apply(cmd.Def.SequenceOptions, false) }, false, false)
	}
	return stateErrorf(stateFeatureNotSupported, "unsupported identity alteration")
}

func sequenceDisplayName(schema, name string) string {
	if isTempSchema(schema) {
		schema = tempAlias
	}
	quote := func(s string) string {
		t, err := lex(s)
		if err == nil && len(t) == 2 && t[0].kind == tokIdent && t[0].text == s && strings.ToLower(s) == s {
			return s
		}
		return quoteSQLName(s)
	}
	return quote(schema) + "." + quote(name)
}

func (e *executor) validateSequenceOwner(ctx context.Context, m *sequenceMeta) error {
	if m.Owned.Name == "" {
		m.OwnerOID, m.OwnerField = 0, ""
		return nil
	}
	if m.Owned.Schema == "" {
		m.Owned.Schema = m.Schema
	}
	table, err := e.loadTable(ctx, m.Owned)
	if err != nil {
		return err
	}
	if table.Schema != m.Schema {
		return stateErrorf(stateInvalidParameter, "sequence and owned table must have the same schema")
	}
	i, ok := table.resolve(m.Column)
	if !ok {
		return stateErrorf(stateUndefinedColumn, "column %q does not exist", m.Column)
	}
	m.Owned = TableName{Schema: table.Schema, Name: table.Name}
	m.OwnerOID, m.OwnerField = tableOID(table), table.Columns[i].field()
	return nil
}
