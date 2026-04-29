package archive

import (
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
)

const (
	BufferSize    = 4096
	DecoderMinHit = 3
	EncoderMinHit = 2
	WindowSize    = 16
	HashSize      = 4096
	Empty         = 0xFFFF
)

// Decompress extracts files from the archive reader.
func (h *ArchiveHeader) Decompress(r io.Reader, baseDir string, testOnly bool) error {
	if h.Version == 2 {
		return h.Decompress2(r, baseDir, testOnly) // Real 0.2.0 Adaptive Huffman
	}

	var asdExtraBuf [1]byte
	if _, err := io.ReadFull(r, asdExtraBuf[:]); err != nil {
		return err
	}
	asdExtra := int(asdExtraBuf[0])

	buffer := make([]byte, BufferSize)
	for i := range buffer {
		buffer[i] = '0'
	}
	bpos := 0

	fileIdx := 0
	var currentFile *os.File
	var currentCRC uint32 = 0
	var bytesRead uint32 = 0

	// Helper to ensure the current file is open
	ensureFileOpen := func() error {
		if fileIdx < len(h.Files) && currentFile == nil && !testOnly {
			path := filepath.Join(baseDir, h.Files[fileIdx].Name)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			f, err := os.Create(path)
			if err != nil {
				return err
			}
			currentFile = f
		}
		return nil
	}

	outputByte := func(b byte) error {
		for fileIdx < len(h.Files) && bytesRead >= h.Files[fileIdx].Size {
			if currentFile != nil {
				if !testOnly {
					currentFile.Close()
				}
				if currentCRC != h.Files[fileIdx].CRC {
					return fmt.Errorf("CRC error in file %s (expected %08X, got %08X)", h.Files[fileIdx].Name, h.Files[fileIdx].CRC, currentCRC)
				}
			}
			fileIdx++
			bytesRead = 0
			currentCRC = 0
			currentFile = nil
		}

		if fileIdx >= len(h.Files) {
			return nil
		}

		if err := ensureFileOpen(); err != nil {
			return err
		}

		if currentFile != nil && !testOnly {
			if _, err := currentFile.Write([]byte{b}); err != nil {
				return err
			}
		}

		currentCRC = crc32.Update(currentCRC, crc32.IEEETable, []byte{b})
		bytesRead++
		buffer[bpos] = b
		bpos = (bpos + 1) % BufferSize
		return nil
	}

	for {
		var controlByteBuf [1]byte
		if _, err := io.ReadFull(r, controlByteBuf[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return err
		}
		control := controlByteBuf[0]

		for k := 128; k >= 1; k >>= 1 {
			if control&byte(k) != 0 {
				var matchBuf [2]byte
				if _, err := io.ReadFull(r, matchBuf[:]); err != nil {
					if err == io.EOF || err == io.ErrUnexpectedEOF {
						goto done
					}
					return err
				}
				i := int(matchBuf[0])
				j := int(matchBuf[1])

				hit := (i >> 4) + DecoderMinHit
				if hit == (15 + DecoderMinHit) {
					hit += asdExtra
				}
				pos := ((i & 0x0f) * 256) + j

				for l := 0; l < hit; l++ {
					matchByte := buffer[(bpos+BufferSize-pos-1)%BufferSize]
					if err := outputByte(matchByte); err != nil {
						return err
					}
				}
			} else {
				var literalBuf [1]byte
				if _, err := io.ReadFull(r, literalBuf[:]); err != nil {
					if err == io.EOF || err == io.ErrUnexpectedEOF {
						goto done
					}
					return err
				}
				if err := outputByte(literalBuf[0]); err != nil {
					return err
				}
			}
			if k == 1 {
				break
			}
		}
	}

done:
	if fileIdx < len(h.Files) {
		if currentFile != nil {
			currentFile.Close()
		}
		if currentCRC != h.Files[fileIdx].CRC {
			return fmt.Errorf("CRC error in last file %s (expected %08X, got %08X)", h.Files[fileIdx].Name, h.Files[fileIdx].CRC, currentCRC)
		}
	}

	return nil
}

// Compress compresses files from r and writes to w.
func Compress(w io.Writer, r io.Reader, asdExtra int, hashDeep int) error {
	if _, err := w.Write([]byte{byte(asdExtra)}); err != nil {
		return err
	}

	wTotal := WindowSize + EncoderMinHit + asdExtra
	window := make([]byte, wTotal)
	buffer := make([]byte, BufferSize)
	for i := range buffer {
		buffer[i] = '0'
	}

	hashNext := make([]uint16, BufferSize)
	hashPrev := make([]uint16, BufferSize)
	hashTable := make([]uint16, HashSize)
	for i := range hashTable {
		hashTable[i] = Empty
	}
	for i := range hashNext {
		hashNext[i] = Empty
		hashPrev[i] = Empty
	}

	getHash := func(c1, c2, c3 byte) int {
		return int(((int(c1) ^ (int(c2) << 3)) ^ int(c3)) & 0xFFF)
	}

	wpos, bpos := 0, 0
	leftInWindow := 0

	// Initial window fill
	for i := 0; i < wTotal; i++ {
		var b [1]byte
		if n, _ := r.Read(b[:]); n > 0 {
			window[i] = b[0]
			leftInWindow++
		} else {
			break
		}
	}

	addToBuffer := func() {
		char := window[wpos%wTotal]
		buffer[bpos] = char

		c1 := buffer[(BufferSize+bpos-2)%BufferSize]
		c2 := buffer[(BufferSize+bpos-1)%BufferSize]
		c3 := buffer[bpos]
		hVal := getHash(c1, c2, c3)

		// Remove old entry from hash table/chains
		prev := hashPrev[bpos]
		if prev != Empty {
			if prev >= 5000 {
				hashTable[prev-5000] = Empty
			} else {
				hashNext[prev] = Empty
			}
		}

		// Insert new entry
		if hashTable[hVal] != Empty {
			hashPrev[hashTable[hVal]] = uint16(bpos)
			hashNext[bpos] = hashTable[hVal]
		} else {
			hashNext[bpos] = Empty
		}
		hashTable[hVal] = uint16(bpos)
		hashPrev[bpos] = uint16(5000 + hVal)

		bpos = (bpos + 1) % BufferSize
	}

	findMatch := func() (int, int) {
		hVal := getHash(window[wpos%wTotal], window[(wpos+1)%wTotal], window[(wpos+2)%wTotal])
		nHash := hashTable[hVal]
		if nHash == Empty {
			return 0, 0
		}

		matchLen, matchPos := 0, 0
		hDeepCount := 0

		for nHash != Empty {
			hDeepCount++
			tempLen := 0
			for tempLen < wTotal {
				if window[(wpos+tempLen)%wTotal] != buffer[(BufferSize+int(nHash)-2+tempLen)%BufferSize] {
					break
				}
				tempLen++
				// Compatibility check with original asd 0.1
				if uint32(tempLen-1) >= uint32(BufferSize+bpos-(int(nHash)-1))%BufferSize {
					break
				}
			}

			if tempLen > matchLen {
				matchLen = tempLen
				matchPos = (BufferSize + bpos - (int(nHash) - 1)) % BufferSize
				if matchLen >= wTotal {
					break
				}
			}

			nHash = hashNext[nHash]
			if hDeepCount >= hashDeep {
				break
			}
		}
		return matchLen, matchPos
	}

	code := make([]byte, 17)
	codeCount := 1
	var mask byte = 128

	for leftInWindow > 0 {
		mLen, mPos := findMatch()

		// Adjust match length to fit decoder's range
		// Original code: window_size + minhit = 16 + 2 = 18
		// Lengths 3-17 are encoded as 0-14. 15 is special (18 + asd_extra).
		if mLen < wTotal && mLen >= (WindowSize+EncoderMinHit) {
			mLen = WindowSize + EncoderMinHit - 1 // 17
		}
		l := mLen
		if l == wTotal {
			l = WindowSize + EncoderMinHit // 18
		}

		if mLen > EncoderMinHit {
			// Emit match
			code[0] |= mask
			code[codeCount] = byte(((l - EncoderMinHit - 1) * 16) + ((mPos & 0x0F00) >> 8))
			codeCount++
			code[codeCount] = byte(mPos & 0x00FF)
			codeCount++

			for i := 0; i < mLen; i++ {
				addToBuffer()
				var b [1]byte
				if n, _ := r.Read(b[:]); n > 0 {
					window[wpos%wTotal] = b[0]
					wpos = (wpos + 1) % wTotal
				} else {
					wpos = (wpos + 1) % wTotal
					leftInWindow--
				}
			}
		} else {
			// Emit literal
			code[codeCount] = window[wpos%wTotal]
			codeCount++

			addToBuffer()
			var b [1]byte
			if n, _ := r.Read(b[:]); n > 0 {
				window[wpos%wTotal] = b[0]
				wpos = (wpos + 1) % wTotal
			} else {
				wpos = (wpos + 1) % wTotal
				leftInWindow--
			}
		}

		mask >>= 1
		if mask == 0 {
			if _, err := w.Write(code[:codeCount]); err != nil {
				return err
			}
			code[0] = 0
			codeCount = 1
			mask = 128
		}
	}

	if codeCount > 1 {
		if _, err := w.Write(code[:codeCount]); err != nil {
			return err
		}
	}

	return nil
}
