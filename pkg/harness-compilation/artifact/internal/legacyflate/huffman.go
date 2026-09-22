package legacyflate

import (
	"math"
	"math/bits"
	"sort"
)

type hcode struct{ code, len uint16 }
type literalNode struct {
	literal uint16
	freq    int32
}
type huffmanEncoder struct {
	codes     []hcode
	freqcache []literalNode
	bitCount  [17]int32
	lns       byLiteral
	lfs       byFreq
}
type levelInfo struct{ level, lastFreq, nextCharFreq, nextPairFreq, needed int32 }

func newHuffmanEncoder(size int) *huffmanEncoder { return &huffmanEncoder{codes: make([]hcode, size)} }
func (h *huffmanEncoder) bitLength(freq []int32) int {
	total := 0
	for i, f := range freq {
		if f != 0 {
			total += int(f) * int(h.codes[i].len)
		}
	}
	return total
}
func (h *huffmanEncoder) bitCounts(list []literalNode, maxBits int32) []int32 {
	const maxBitsLimit = 16
	if maxBits >= maxBitsLimit {
		panic("flate: maxBits too large")
	}
	n := int32(len(list))
	list = list[:n+1]
	list[n] = literalNode{math.MaxUint16, math.MaxInt32}
	if maxBits > n-1 {
		maxBits = n - 1
	}
	var levels [maxBitsLimit]levelInfo
	var leafCounts [maxBitsLimit][maxBitsLimit]int32
	for level := int32(1); level <= maxBits; level++ {
		levels[level] = levelInfo{level: level, lastFreq: list[1].freq, nextCharFreq: list[2].freq, nextPairFreq: list[0].freq + list[1].freq}
		leafCounts[level][level] = 2
		if level == 1 {
			levels[level].nextPairFreq = math.MaxInt32
		}
	}
	levels[maxBits].needed = 2*n - 4
	level := maxBits
	for {
		l := &levels[level]
		if l.nextPairFreq == math.MaxInt32 && l.nextCharFreq == math.MaxInt32 {
			l.needed = 0
			levels[level+1].nextPairFreq = math.MaxInt32
			level++
			continue
		}
		prevFreq := l.lastFreq
		if l.nextCharFreq < l.nextPairFreq {
			n := leafCounts[level][level] + 1
			l.lastFreq = l.nextCharFreq
			leafCounts[level][level] = n
			l.nextCharFreq = list[n].freq
		} else {
			l.lastFreq = l.nextPairFreq
			copy(leafCounts[level][:level], leafCounts[level-1][:level])
			levels[l.level-1].needed = 2
		}
		if l.needed--; l.needed == 0 {
			if l.level == maxBits {
				break
			}
			levels[l.level+1].nextPairFreq = prevFreq + l.lastFreq
			level++
		} else {
			for levels[level-1].needed > 0 {
				level--
			}
		}
	}
	if leafCounts[maxBits][maxBits] != n {
		panic("leafCounts[maxBits][maxBits] != n")
	}
	bitCount := h.bitCount[:maxBits+1]
	nbits := 1
	counts := &leafCounts[maxBits]
	for level := maxBits; level > 0; level-- {
		bitCount[nbits] = counts[level] - counts[level-1]
		nbits++
	}
	return bitCount
}
func (h *huffmanEncoder) generate(freq []int32, maxBits int32) {
	if h.freqcache == nil {
		h.freqcache = make([]literalNode, maxNumLit+1)
	}
	list := h.freqcache[:len(freq)+1]
	count := 0
	for i, f := range freq {
		if f != 0 {
			list[count] = literalNode{uint16(i), f}
			count++
		} else {
			h.codes[i].len = 0
		}
	}
	list = list[:count]
	if count <= 2 {
		for i, node := range list {
			h.codes[node.literal] = hcode{uint16(i), 1}
		}
		return
	}
	h.lfs = list
	sort.Sort(&h.lfs)
	bitCount := h.bitCounts(list, maxBits)
	code := uint16(0)
	for n, nbits := range bitCount {
		code <<= 1
		if n == 0 || nbits == 0 {
			continue
		}
		chunk := list[len(list)-int(nbits):]
		h.lns = chunk
		sort.Sort(&h.lns)
		for _, node := range chunk {
			h.codes[node.literal] = hcode{bits.Reverse16(code << (16 - uint(n))), uint16(n)}
			code++
		}
		list = list[:len(list)-int(nbits)]
	}
}

type byLiteral []literalNode

func (s byLiteral) Len() int           { return len(s) }
func (s byLiteral) Less(i, j int) bool { return s[i].literal < s[j].literal }
func (s byLiteral) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

type byFreq []literalNode

func (s byFreq) Len() int { return len(s) }
func (s byFreq) Less(i, j int) bool {
	if s[i].freq == s[j].freq {
		return s[i].literal < s[j].literal
	}
	return s[i].freq < s[j].freq
}
func (s byFreq) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
