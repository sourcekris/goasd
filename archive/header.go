package archive

import (
	"encoding/binary"
	"fmt"
	"io"
)

const Signature01 = "ASD01\x1a"
const Signature02 = "ASD02\x1a"

type FileEntry struct {
	Name      string
	Size      uint32
	CRC       uint32
	Time      uint32
	Attribute uint16
}

type ArchiveHeader struct {
	Version int
	Files   []FileEntry
}

// ReadHeader reads the ASD archive signature and file metadata list.
func ReadHeader(r io.Reader) (*ArchiveHeader, error) {
	sig := make([]byte, 6)
	if _, err := io.ReadFull(r, sig); err != nil {
		return nil, err
	}

	var version int
	if string(sig) == Signature01 {
		version = 1
	} else if string(sig) == Signature02 {
		version = 2
	} else {
		return nil, fmt.Errorf("not an ASD archive (invalid signature: %q)", sig)
	}

	var numFilesLow uint16
	if err := binary.Read(r, binary.LittleEndian, &numFilesLow); err != nil {
		return nil, err
	}

	numFiles := uint32(numFilesLow)

	if version == 2 {
		var highByte uint8
		if err := binary.Read(r, binary.LittleEndian, &highByte); err != nil {
			return nil, err
		}
		numFiles += uint32(highByte) * 0x10000
	}

	header := &ArchiveHeader{
		Version: version,
		Files:   make([]FileEntry, numFiles),
	}

	for i := 0; i < int(numFiles); i++ {
		var nameLen uint8
		if err := binary.Read(r, binary.LittleEndian, &nameLen); err != nil {
			return nil, err
		}

		nameBuf := make([]byte, nameLen)
		if _, err := io.ReadFull(r, nameBuf); err != nil {
			return nil, err
		}

		entry := FileEntry{
			Name: string(nameBuf),
		}

		if err := binary.Read(r, binary.LittleEndian, &entry.Size); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &entry.CRC); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &entry.Time); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &entry.Attribute); err != nil {
			return nil, err
		}

		header.Files[i] = entry
	}

	return header, nil
}

// WriteHeader writes the ASD archive signature and file metadata list.
func (h *ArchiveHeader) WriteHeader(w io.Writer) error {
	sig := Signature01
	if h.Version == 2 {
		sig = Signature02
	}

	if _, err := w.Write([]byte(sig)); err != nil {
		return err
	}

	numFiles := uint16(len(h.Files))
	if err := binary.Write(w, binary.LittleEndian, numFiles); err != nil {
		return err
	}

	if h.Version == 2 {
		highByte := uint8((len(h.Files) >> 16) & 0xFF)
		if err := binary.Write(w, binary.LittleEndian, highByte); err != nil {
			return err
		}
	}

	for _, entry := range h.Files {
		nameLen := uint8(len(entry.Name))
		if err := binary.Write(w, binary.LittleEndian, nameLen); err != nil {
			return err
		}
		if _, err := w.Write([]byte(entry.Name)); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, entry.Size); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, entry.CRC); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, entry.Time); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, entry.Attribute); err != nil {
			return err
		}
	}

	return nil
}
