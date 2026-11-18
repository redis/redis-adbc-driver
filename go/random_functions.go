// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"math"
	"math/bits"
	"strings"
	"sync"
)

// SQL's seeded random stream follows PostgreSQL18's xoroshiro128** and
// splitmix64 initialization (see random-PostgreSQL-LICENSE.txt). The lock spans
// an entire SQL draw, including both uniforms used by Box-Muller sampling.
// This stream is session-local and separate from cryptographic UUID generation.
type sqlRandom struct {
	mu          sync.Mutex
	s0, s1      uint64
	initialized bool
}

func (r *sqlRandom) seed(seed uint64) {
	split := func() uint64 {
		seed += 0x9e3779b97f4a7c15
		v := seed
		v = (v ^ (v >> 30)) * 0xbf58476d1ce4e5b9
		v = (v ^ (v >> 27)) * 0x94d049bb133111eb
		return v ^ (v >> 31)
	}
	r.s0, r.s1 = split(), split()
	if r.s0 == 0 && r.s1 == 0 {
		r.s0, r.s1 = 0x5851f42d4c957f2d, 0x14057b7ef767814f
	}
	r.initialized = true
}
func (r *sqlRandom) initialize() error {
	if !r.initialized {
		var seed [16]byte
		if _, err := cryptorand.Read(seed[:]); err != nil {
			return err
		}
		r.s0, r.s1 = binary.LittleEndian.Uint64(seed[:8]), binary.LittleEndian.Uint64(seed[8:])
		if r.s0 == 0 && r.s1 == 0 {
			r.seed(0)
		}
		r.initialized = true
	}
	return nil
}
func (r *sqlRandom) uniform() float64 {
	s0, sx := r.s0, r.s1^r.s0
	value := bits.RotateLeft64(s0*5, 7) * 9
	r.s0 = bits.RotateLeft64(s0, 24) ^ sx ^ (sx << 16)
	r.s1 = bits.RotateLeft64(sx, 37)
	return float64(value>>12) * (1.0 / (1 << 52))
}
func (r *sqlRandom) setSeed(seed float64) error {
	if math.IsNaN(seed) || seed < -1 || seed > 1 {
		return stateErr(stateInvalidParameter, "setseed parameter %g is out of allowed range [-1,1]", seed)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seed(uint64(int64(float64((uint64(1)<<52)-1) * seed)))
	return nil
}
func (r *sqlRandom) draw(normal bool, mean, stddev float64) (float64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.initialize(); err != nil {
		return 0, err
	}
	if !normal {
		return r.uniform(), nil
	}
	u1, u2 := 1-r.uniform(), 1-r.uniform()
	z := math.Sqrt(-2*math.Log(u1)) * math.Sin(2*math.Pi*u2)
	return stddev*z + mean, nil
}

var typeVoid = ColType{Kind: KindString, Alias: "void"}

func randomFunction(name string) bool {
	return name == "SETSEED" || name == "RANDOM_NORMAL" || name == "RANDOM"
}

func randomCallError(f *Func, args []ColType) error {
	if f.Name != "SETSEED" && f.Name != "RANDOM_NORMAL" {
		return nil
	}
	for i, t := range args {
		if t.Kind != KindNull && !t.Kind.isNumeric() && !(i < len(f.Args) && unknownLiteral(f.Args[i])) {
			return noSuchFunction(f.Name, false, args)
		}
		if i < len(f.Args) && unknownLiteral(f.Args[i]) {
			if err := checkLiteral(f.Args[i], typeFloat64); err != nil {
				return err
			}
		}
	}
	return nil
}

func (env *evalEnv) evalRandom(f *Func, args []Value) (Value, error) {
	if err := randomCallError(f, argTypes(args)); err != nil {
		return Value{}, err
	}
	typ := typeFloat64
	if f.Name == "SETSEED" {
		typ = typeVoid
	}
	for _, v := range args {
		if v.Null {
			return nullValue(typ), nil
		}
	}
	floats := make([]float64, len(args))
	for i, v := range args {
		converted, err := coerceIn(v, typeFloat64, env.zone())
		if err != nil {
			return Value{}, err
		}
		floats[i] = converted.F
	}
	// In-memory evaluation without a connection still owns a private stream.
	// A real connection always supplies its session through the executor.
	if env.exec == nil {
		env.exec = &executor{sess: newSession(defaultSchema)}
	}
	if env.exec.sess == nil {
		env.exec.sess = newSession(defaultSchema)
	}
	random := &env.exec.sess.random
	if f.Name == "SETSEED" {
		if err := random.setSeed(floats[0]); err != nil {
			return Value{}, err
		}
		return Value{T: typeVoid, S: ""}, nil
	}
	mean, stddev := 0.0, 1.0
	if len(floats) > 0 {
		mean = floats[0]
	}
	if len(floats) > 1 {
		stddev = floats[1]
	}
	out, err := random.draw(f.Name == "RANDOM_NORMAL", mean, stddev)
	return floatValue(typeFloat64, out), err
}

// void is represented physically as an empty Arrow string, but does not gain
// text's implicit overloads. Functions whose PostgreSQL signature takes any
// type retain the logical void value; explicit casts to text remain available.
func voidCallError(f *Func, args []ColType) error {
	for i, t := range args {
		if t.Alias != "void" {
			continue
		}
		switch f.Name {
		case "PG_TYPEOF", "COALESCE", "CONCAT", "NUM_NULLS", "NUM_NONNULLS", "QUOTE_LITERAL", "QUOTE_NULLABLE", "COUNT":
		case "FORMAT", "CONCAT_WS":
			if i == 0 {
				return noSuchFunction(f.Name, false, args)
			}
		default:
			return noSuchFunction(f.Name, false, args)
		}
	}
	return nil
}
func containsSetseed(x Expr) bool {
	found := false
	walkExpr(x, func(e Expr) {
		if f, ok := e.(*Func); ok && f.Name == "SETSEED" {
			found = true
		}
	})
	return found
}

// RANDOM_NORMAL's two defaulted named arguments are PostgreSQL's mean and
// stddev. SETSEED's single input has no SQL argument name.
func (p *parser) parseRandomNormal() (Expr, error) {
	f := &Func{Name: "RANDOM_NORMAL", named: true, Args: []Expr{&Literal{V: floatValue(typeFloat64, 0)}, &Literal{V: floatValue(typeFloat64, 1)}}}
	used := [2]bool{}
	namedSlots := [2]bool{}
	pos, named := 0, false
	if !p.isOp(")") {
		for {
			slot := pos
			isNamed := false
			t := p.peek()
			if (t.kind == tokIdent || t.kind == tokQuotedIdent) && (p.peekAt(1).text == "=>" || p.peekAt(1).text == ":=") {
				p.pos += 2
				name := t.text
				if t.kind == tokIdent {
					name = strings.ToLower(name)
				}
				switch name {
				case "mean":
					slot = 0
				case "stddev":
					slot = 1
				default:
					return nil, stateErr(stateUndefinedFunction, "function random_normal has no argument named %q", name)
				}
				named = true
				isNamed = true
			} else if named {
				return nil, syntaxErr("positional argument cannot follow named argument")
			}
			if slot >= 2 {
				return nil, stateErr(stateUndefinedFunction, "too many arguments for random_normal")
			}
			if used[slot] {
				if !namedSlots[slot] {
					return nil, stateErr(stateUndefinedFunction, "function random_normal has overlapping positional and named arguments")
				}
				return nil, syntaxErr("argument name used more than once")
			}
			used[slot] = true
			namedSlots[slot] = isNamed
			x, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			f.Args[slot] = x
			pos++
			if !p.acceptOp(",") {
				break
			}
		}
	}
	if err := p.expectOp(")"); err != nil {
		return nil, err
	}
	return f, nil
}
