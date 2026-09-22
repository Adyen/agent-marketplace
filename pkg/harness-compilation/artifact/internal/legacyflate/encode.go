package legacyflate

import "encoding/binary"

const maxNumLit = 286
const endBlockMarker = 256
const lengthCodesStart = 257
const badCode = 255

var lengthExtraBits = []uint{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0}
var lengthBase = []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 10, 12, 14, 16, 20, 24, 28, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 255}
var offsetExtraBits = []uint{0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13}
var offsetBase = []uint32{0, 1, 2, 3, 4, 6, 8, 12, 16, 24, 32, 48, 64, 96, 128, 192, 256, 384, 512, 768, 1024, 1536, 2048, 3072, 4096, 6144, 8192, 12288, 16384, 24576}
var codegenOrder = []int{16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15}

type bitWriter struct {
	out                                              []byte
	bits                                             uint64
	nbits                                            uint
	literalFreq                                      [maxNumLit]int32
	offsetFreq                                       [30]int32
	codegenFreq                                      [19]int32
	codegen                                          [maxNumLit + 31]uint8
	literalEncoding, offsetEncoding, codegenEncoding *huffmanEncoder
}

func (w *bitWriter) writeBits(value uint32, n uint) {
	w.bits |= uint64(value) << w.nbits
	w.nbits += n
	for w.nbits >= 8 {
		w.out = append(w.out, byte(w.bits))
		w.bits >>= 8
		w.nbits -= 8
	}
}
func (w *bitWriter) writeCode(c hcode) { w.writeBits(uint32(c.code), uint(c.len)) }
func (w *bitWriter) flush() {
	if w.nbits > 0 {
		w.out = append(w.out, byte(w.bits))
		w.bits = 0
		w.nbits = 0
	}
}
func (w *bitWriter) stored(input []byte, final bool) {
	flag := uint32(0)
	if final {
		flag = 1
	}
	w.writeBits(flag, 3)
	w.flush()
	w.out = binary.LittleEndian.AppendUint16(w.out, uint16(len(input)))
	w.out = binary.LittleEndian.AppendUint16(w.out, ^uint16(len(input)))
	w.out = append(w.out, input...)
}
func (w *bitWriter) generateCodegen(nl, no int, lit, off *huffmanEncoder) {
	clear(w.codegenFreq[:])
	cg := w.codegen[:]
	for i := 0; i < nl; i++ {
		cg[i] = uint8(lit.codes[i].len)
	}
	for i := 0; i < no; i++ {
		cg[nl+i] = uint8(off.codes[i].len)
	}
	cg[nl+no] = badCode
	size := cg[0]
	count := 1
	out := 0
	for in := 1; size != badCode; in++ {
		next := cg[in]
		if next == size {
			count++
			continue
		}
		if size != 0 {
			cg[out] = size
			out++
			w.codegenFreq[size]++
			count--
			for count >= 3 {
				n := min(6, count)
				cg[out] = 16
				cg[out+1] = uint8(n - 3)
				out += 2
				w.codegenFreq[16]++
				count -= n
			}
		} else {
			for count >= 11 {
				n := min(138, count)
				cg[out] = 18
				cg[out+1] = uint8(n - 11)
				out += 2
				w.codegenFreq[18]++
				count -= n
			}
			if count >= 3 {
				cg[out] = 17
				cg[out+1] = uint8(count - 3)
				out += 2
				w.codegenFreq[17]++
				count = 0
			}
		}
		for count--; count >= 0; count-- {
			cg[out] = size
			out++
			w.codegenFreq[size]++
		}
		size = next
		count = 1
	}
	cg[out] = badCode
}
func (w *bitWriter) dynamicSize(lit, off *huffmanEncoder) (int, int) {
	n := len(w.codegenFreq)
	for n > 4 && w.codegenFreq[codegenOrder[n-1]] == 0 {
		n--
	}
	header := 3 + 5 + 5 + 4 + 3*n + w.codegenEncoding.bitLength(w.codegenFreq[:]) + int(w.codegenFreq[16])*2 + int(w.codegenFreq[17])*3 + int(w.codegenFreq[18])*7
	return header + lit.bitLength(w.literalFreq[:]) + off.bitLength(w.offsetFreq[:]), n
}
func (w *bitWriter) header(nl, no, nc int) {
	w.writeBits(4, 3)
	w.writeBits(uint32(nl-257), 5)
	w.writeBits(uint32(no-1), 5)
	w.writeBits(uint32(nc-4), 4)
	for i := 0; i < nc; i++ {
		w.writeBits(uint32(w.codegenEncoding.codes[codegenOrder[i]].len), 3)
	}
	for i := 0; w.codegen[i] != badCode; i++ {
		code := w.codegen[i]
		w.writeCode(w.codegenEncoding.codes[code])
		switch code {
		case 16:
			i++
			w.writeBits(uint32(w.codegen[i]), 2)
		case 17:
			i++
			w.writeBits(uint32(w.codegen[i]), 3)
		case 18:
			i++
			w.writeBits(uint32(w.codegen[i]), 7)
		}
	}
}
func (w *bitWriter) dynamic(tokens []token, input []byte) {
	tokens = append(tokens, endBlockMarker)
	clear(w.literalFreq[:])
	clear(w.offsetFreq[:])
	for _, t := range tokens {
		if t < matchType {
			w.literalFreq[t.literal()]++
		} else {
			w.literalFreq[lengthCodesStart+lengthCode(t.length())]++
			w.offsetFreq[offsetCode(t.offset())]++
		}
	}
	nl := len(w.literalFreq)
	for w.literalFreq[nl-1] == 0 {
		nl--
	}
	no := len(w.offsetFreq)
	for no > 0 && w.offsetFreq[no-1] == 0 {
		no--
	}
	if no == 0 {
		w.offsetFreq[0] = 1
		no = 1
	}
	w.literalEncoding.generate(w.literalFreq[:], 15)
	w.offsetEncoding.generate(w.offsetFreq[:], 15)
	w.generateCodegen(nl, no, w.literalEncoding, w.offsetEncoding)
	w.codegenEncoding.generate(w.codegenFreq[:], 7)
	size, nc := w.dynamicSize(w.literalEncoding, w.offsetEncoding)
	if (len(input)+5)*8 < size+(size>>4) {
		w.stored(input, false)
		return
	}
	w.header(nl, no, nc)
	for _, t := range tokens {
		if t < matchType {
			w.writeCode(w.literalEncoding.codes[t.literal()])
			continue
		}
		l := t.length()
		lc := lengthCode(l)
		w.writeCode(w.literalEncoding.codes[lc+lengthCodesStart])
		if n := lengthExtraBits[lc]; n > 0 {
			w.writeBits(l-lengthBase[lc], n)
		}
		o := t.offset()
		oc := offsetCode(o)
		w.writeCode(w.offsetEncoding.codes[oc])
		if n := offsetExtraBits[oc]; n > 0 {
			w.writeBits(o-offsetBase[oc], n)
		}
	}
}
func (w *bitWriter) huff(input []byte) {
	clear(w.literalFreq[:])
	for _, b := range input {
		w.literalFreq[b]++
	}
	w.literalFreq[endBlockMarker] = 1
	w.offsetFreq[0] = 1
	w.literalEncoding.generate(w.literalFreq[:], 15)
	off := newHuffmanEncoder(30)
	off.codes[0] = hcode{0, 1}
	w.generateCodegen(257, 1, w.literalEncoding, off)
	w.codegenEncoding.generate(w.codegenFreq[:], 7)
	size, nc := w.dynamicSize(w.literalEncoding, off)
	if (len(input)+5)*8 < size+(size>>4) {
		w.stored(input, false)
		return
	}
	w.header(257, 1, nc)
	for _, b := range input {
		w.writeCode(w.literalEncoding.codes[b])
	}
	w.writeCode(w.literalEncoding.codes[endBlockMarker])
}
func Encode(input []byte) []byte {
	w := bitWriter{literalEncoding: newHuffmanEncoder(maxNumLit), offsetEncoding: newHuffmanEncoder(30), codegenEncoding: newHuffmanEncoder(19)}
	fast := newDeflateFast()
	tokens := make([]token, 0, maxStoreBlockSize)
	for len(input) > 0 {
		n := min(len(input), maxStoreBlockSize)
		block := input[:n]
		input = input[n:]
		switch {
		case n <= 16:
			w.stored(block, false)
		case n < 128:
			w.huff(block)
		default:
			tokens = fast.encode(tokens[:0], block)
			if len(tokens) > n-(n>>4) {
				w.huff(block)
			} else {
				w.dynamic(tokens, block)
			}
		}
	}
	w.stored(nil, true)
	return w.out
}
