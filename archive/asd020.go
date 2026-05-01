package archive

import (
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type BitReader struct {
	r    io.Reader
	b    byte
	mask byte
}

func NewBitReader(r io.Reader) *BitReader {
	return &BitReader{r: r, mask: 0x80}
}

func (br *BitReader) ReadBit() (uint32, error) {
	if br.mask == 0x80 {
		var buf [1]byte
		_, err := io.ReadFull(br.r, buf[:])
		if err != nil {
			return 0xFFFFFFFF, err
		}
		br.b = buf[0]
	}
	var bit uint32 = 0
	if (br.b & br.mask) != 0 {
		bit = 1
	}
	br.mask >>= 1
	if br.mask == 0 {
		br.mask = 0x80
	}
	return bit, nil
}

func (br *BitReader) ReadBits(n int) (uint32, error) {
	mask := uint32(1 << (n - 1))
	var res uint32 = 0
	for {
		if mask == 0 {
			return res, nil
		}

		if br.mask == 0x80 {
			var buf [1]byte
			_, err := io.ReadFull(br.r, buf[:])
			if err != nil {
				return 0xFFFFFFFF, err
			}
			br.b = buf[0]
		}

		if (br.b & br.mask) != 0 {
			res |= mask
		}
		mask >>= 1
		br.mask >>= 1
		if br.mask == 0 {
			br.mask = 0x80
		}
	}
}

type HuffTree struct {
	NumCodes     int
	Count        []uint32
	HighTree     []int
	LowTree      []int
	FromTree     []int
	Counted      []bool
	HuffCounter  int
	ScaleCounter int
	Rehuff       int
	Rescale      int
}

func NewHuffTree(numCodes, rehuffFactor, rescaleFactor int) *HuffTree {
	t := &HuffTree{
		NumCodes: numCodes,
		Count:    make([]uint32, numCodes*2),
		HighTree: make([]int, numCodes),
		LowTree:  make([]int, numCodes),
		FromTree: make([]int, numCodes*2),
		Counted:  make([]bool, numCodes*2),
		Rehuff:   rehuffFactor * numCodes,
		Rescale:  rescaleFactor * numCodes,
	}
	for i := 0; i < numCodes; i++ {
		t.Count[i] = 1
	}
	t.BuildTree()
	return t
}

func (t *HuffTree) GetLowest() int {
	minCount := uint32(0xFFFFFFFF)
	minIndex := 0
	for i := 0; i < t.NumCodes*2; i++ {
		if !t.Counted[i] && t.Count[i] != 0 && t.Count[i] < minCount {
			minCount = t.Count[i]
			minIndex = i
		}
	}
	return minIndex
}

func (t *HuffTree) BuildTree() {
	for i := 0; i < t.NumCodes*2; i++ {
		t.Counted[i] = false
	}
	for i := t.NumCodes; i < t.NumCodes*2; i++ {
		t.Count[i] = 0
	}

	nextNode := t.NumCodes
	for nodesLeft := t.NumCodes; nodesLeft > 1; nodesLeft-- {
		idx1 := t.GetLowest()
		count1 := t.Count[idx1]
		t.Counted[idx1] = true

		idx2 := t.GetLowest()
		t.Count[nextNode] = t.Count[idx2] + count1
		t.Counted[idx2] = true

		t.LowTree[nextNode-t.NumCodes] = idx1
		t.HighTree[nextNode-t.NumCodes] = idx2
		t.FromTree[idx1] = nextNode
		t.FromTree[idx2] = nextNode
		nextNode++
	}
}

func (t *HuffTree) RescaleCounts() {
	for i := 0; i < t.NumCodes; i++ {
		t.Count[i] >>= 1
		if t.Count[i] == 0 {
			t.Count[i] = 1
		}
	}
}

func (t *HuffTree) GetCode(br *BitReader) (int, error) {
	node := t.NumCodes*2 - 2
	for {
		bit, err := br.ReadBit()
		if err != nil {
			return -1, err
		}
		if bit == 0xFFFFFFFF {
			return -1, io.EOF
		}

		if bit == 1 {
			node = t.HighTree[node-t.NumCodes]
		} else {
			node = t.LowTree[node-t.NumCodes]
		}
		if node < t.NumCodes {
			break
		}
	}

	t.Count[node]++
	t.HuffCounter++
	t.ScaleCounter++

	if t.HuffCounter > t.Rehuff {
		t.BuildTree()
		t.HuffCounter = 0
	}
	if t.ScaleCounter > t.Rescale {
		t.ScaleCounter = 0
		t.RescaleCounts()
	}
	return node, nil
}

// Decompress2 extracts files from a REAL Version 2 archive (Adaptive Huffman LZSS).
func (h *ArchiveHeader) Decompress2(r io.Reader, baseDir string, testOnly bool) error {
	br := NewBitReader(r)

	minHit := 2
	windowBits, err := br.ReadBits(4)
	if err != nil || windowBits == 0xFFFFFFFF {
		return io.EOF
	}
	bufferBits, _ := br.ReadBits(5)
	offRescale, _ := br.ReadBits(7)
	offRehuff, _ := br.ReadBits(7)
	litRescale, _ := br.ReadBits(7)
	litRehuff, _ := br.ReadBits(7)

	bufferSize := 1 << bufferBits
	buffer := make([]byte, bufferSize+minHit+1024)
	for i := range buffer {
		buffer[i] = '0'
	}

	litNumCodes := int(windowBits*4 + 252)
	offNumCodes := int(bufferBits * 2)

	litTree := NewHuffTree(litNumCodes, int(litRehuff), int(litRescale))
	offTree := NewHuffTree(offNumCodes, int(offRehuff), int(offRescale))

	bpos := 0
	fileIdx := 0
	var currentFile *os.File
	var currentCRC uint32 = 0
	var bytesRead uint32 = 0

	ensureFileOpen := func() error {
		if fileIdx < len(h.Files) && currentFile == nil && !testOnly {
			name := h.Files[fileIdx].Name
			// Normalize separators: treat both / and \ as separators
			name = strings.ReplaceAll(name, "\\", string(os.PathSeparator))
			name = strings.ReplaceAll(name, "/", string(os.PathSeparator))

			path := filepath.Join(baseDir, name)
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
		bpos = (bpos + 1) % bufferSize
		return nil
	}

	for {
		litCode, err := litTree.GetCode(br)
		if err != nil || litCode == -1 {
			break
		}

		if litCode < 0x100 {
			if err := outputByte(byte(litCode)); err != nil {
				return err
			}
		} else {
			matchLenCode := uint32(litCode)
			if matchLenCode < 0x108 {
				matchLenCode -= 0x100
			} else {
				matchLenCode -= 0x108
				extraBits := int((matchLenCode >> 2) + 1)
				extra, err := br.ReadBits(extraBits)
				if err != nil || extra == 0xFFFFFFFF {
					break
				}
				matchLenCode = (1 << ((matchLenCode >> 2) + 3)) + extra + ((matchLenCode & 3) << ((matchLenCode >> 2) + 1))
			}

			offCode, err := offTree.GetCode(br)
			if err != nil || offCode == -1 {
				break
			}

			var matchOffset uint32
			if offCode < 4 {
				extra, err := br.ReadBits(2)
				if err != nil || extra == 0xFFFFFFFF {
					break
				}
				matchOffset = extra
			} else {
				extraBits := int((uint32(offCode) - 2) >> 1)
				extra, err := br.ReadBits(extraBits)
				if err != nil || extra == 0xFFFFFFFF {
					break
				}
				matchOffset = (1 << (uint32(offCode) >> 1)) + ((uint32(offCode) & 1) << ((uint32(offCode) - 2) >> 1)) + extra
			}

			matchLen := minHit + int(matchLenCode)
			for l := 0; l <= matchLen; l++ {
				bufIdx := (bpos + bufferSize - int(matchOffset) - 3) % bufferSize
				c := buffer[bufIdx]
				if err := outputByte(c); err != nil {
					return err
				}
			}
		}
	}

	for fileIdx < len(h.Files) {
		if currentFile != nil {
			currentFile.Close()
		}
		if currentCRC != h.Files[fileIdx].CRC {
			return fmt.Errorf("CRC error in last file %s (expected %08X, got %08X)", h.Files[fileIdx].Name, h.Files[fileIdx].CRC, currentCRC)
		}
		fileIdx++
	}

	return nil
}
