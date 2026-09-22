package legacyflate

import "math"

const (
	maxStoreBlockSize      = 65535
	maxMatchOffset         = 1 << 15
	maxMatchLength         = 258
	baseMatchLength        = 3
	baseMatchOffset        = 1
	tableBits              = 14
	tableSize              = 1 << tableBits
	tableMask              = tableSize - 1
	tableShift             = 32 - tableBits
	bufferReset            = math.MaxInt32 - maxStoreBlockSize*2
	inputMargin            = 15
	minNonLiteralBlockSize = 17
	lengthShift            = 22
	offsetMask             = 1<<lengthShift - 1
	matchType              = 1 << 30
)

type token uint32

func literalToken(v uint32) token            { return token(v) }
func matchToken(length, offset uint32) token { return token(matchType + length<<lengthShift + offset) }
func (t token) literal() uint32              { return uint32(t) }
func (t token) length() uint32               { return uint32(t-matchType) >> lengthShift }
func (t token) offset() uint32               { return uint32(t) & offsetMask }
func lengthCode(n uint32) uint32 {
	if n == 255 {
		return 28
	}
	for i := len(lengthBase) - 2; i >= 0; i-- {
		if n >= lengthBase[i] {
			return uint32(i)
		}
	}
	return 0
}
func offsetCode(n uint32) uint32 {
	for i := len(offsetBase) - 1; i >= 0; i-- {
		if n >= offsetBase[i] {
			return uint32(i)
		}
	}
	return 0
}

type tableEntry struct {
	val    uint32
	offset int32
}
type deflateFast struct {
	table [tableSize]tableEntry
	prev  []byte
	cur   int32
}

func newDeflateFast() *deflateFast {
	return &deflateFast{cur: maxStoreBlockSize, prev: make([]byte, 0, maxStoreBlockSize)}
}
func load32(b []byte, i int32) uint32 {
	b = b[i : i+4 : len(b)]
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
func load64(b []byte, i int32) uint64 {
	b = b[i : i+8 : len(b)]
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 | uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}
func hash(u uint32) uint32 { return (u * 0x1e35a7bd) >> tableShift }
func emitLiteral(dst []token, lit []byte) []token {
	for _, v := range lit {
		dst = append(dst, literalToken(uint32(v)))
	}
	return dst
}
func (e *deflateFast) encode(dst []token, src []byte) []token {
	if e.cur >= bufferReset {
		e.shiftOffsets()
	}
	if len(src) < minNonLiteralBlockSize {
		e.cur += maxStoreBlockSize
		e.prev = e.prev[:0]
		return emitLiteral(dst, src)
	}
	sLimit := int32(len(src) - inputMargin)
	nextEmit := int32(0)
	s := int32(0)
	cv := load32(src, s)
	nextHash := hash(cv)
	for {
		skip := int32(32)
		nextS := s
		var candidate tableEntry
		for {
			s = nextS
			between := skip >> 5
			nextS = s + between
			skip += between
			if nextS > sLimit {
				goto emitRemainder
			}
			candidate = e.table[nextHash&tableMask]
			now := load32(src, nextS)
			e.table[nextHash&tableMask] = tableEntry{offset: s + e.cur, val: cv}
			nextHash = hash(now)
			offset := s - (candidate.offset - e.cur)
			if offset > maxMatchOffset || cv != candidate.val {
				cv = now
				continue
			}
			break
		}
		dst = emitLiteral(dst, src[nextEmit:s])
		for {
			s += 4
			t := candidate.offset - e.cur + 4
			l := e.matchLen(s, t, src)
			dst = append(dst, matchToken(uint32(l+4-baseMatchLength), uint32(s-t-baseMatchOffset)))
			s += l
			nextEmit = s
			if s >= sLimit {
				goto emitRemainder
			}
			x := load64(src, s-1)
			prevHash := hash(uint32(x))
			e.table[prevHash&tableMask] = tableEntry{offset: e.cur + s - 1, val: uint32(x)}
			x >>= 8
			currHash := hash(uint32(x))
			candidate = e.table[currHash&tableMask]
			e.table[currHash&tableMask] = tableEntry{offset: e.cur + s, val: uint32(x)}
			offset := s - (candidate.offset - e.cur)
			if offset > maxMatchOffset || uint32(x) != candidate.val {
				cv = uint32(x >> 8)
				nextHash = hash(cv)
				s++
				break
			}
		}
	}
emitRemainder:
	if int(nextEmit) < len(src) {
		dst = emitLiteral(dst, src[nextEmit:])
	}
	e.cur += int32(len(src))
	e.prev = e.prev[:len(src)]
	copy(e.prev, src)
	return dst
}
func (e *deflateFast) matchLen(s, t int32, src []byte) int32 {
	s1 := min(int(s)+maxMatchLength-4, len(src))
	if t >= 0 {
		b := src[t:]
		a := src[s:s1]
		b = b[:len(a)]
		for i := range a {
			if a[i] != b[i] {
				return int32(i)
			}
		}
		return int32(len(a))
	}
	tp := int32(len(e.prev)) + t
	if tp < 0 {
		return 0
	}
	a := src[s:s1]
	b := e.prev[tp:]
	if len(b) > len(a) {
		b = b[:len(a)]
	}
	a = a[:len(b)]
	for i := range b {
		if a[i] != b[i] {
			return int32(i)
		}
	}
	n := int32(len(b))
	if int(s+n) == s1 {
		return n
	}
	a = src[s+n : s1]
	b = src[:len(a)]
	for i := range a {
		if a[i] != b[i] {
			return int32(i) + n
		}
	}
	return int32(len(a)) + n
}
func (e *deflateFast) shiftOffsets() {
	if len(e.prev) == 0 {
		clear(e.table[:])
		e.cur = maxMatchOffset + 1
		return
	}
	for i := range e.table {
		v := e.table[i].offset - e.cur + maxMatchOffset + 1
		if v < 0 {
			v = 0
		}
		e.table[i].offset = v
	}
	e.cur = maxMatchOffset + 1
}
