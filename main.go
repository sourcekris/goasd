package main

import (
	"flag"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sewid/goasd/archive"
)

type Options struct {
	AssumeYes bool
	Fast      bool
	Max       bool
	NoAttrs   bool
	Recursive bool
}

func usage() {
	fmt.Println("ASD - archiver version 0.1.5 (Golang Port with 0.2.0 Decompression)")
	fmt.Println("Usage:    goasd <option> [<switch(es)>] <arc name> <files...>")
	fmt.Println("Examples: goasd a -m archive.asd *.exe, goasd x -y arch.asd, goasd l arch.asd")
	fmt.Println("\n<Options:>")
	fmt.Println("  a  = add files to archive           l  = list files in archive")
	fmt.Println("  x  = extract files from archive     t  = test files in archive")
	fmt.Println("  h  = Help")
	fmt.Println("\n<Switches:>")
	fmt.Println("  -y = Assume YES on all queries      -f = Set fast compression mode")
	fmt.Println("  -m = Set maximum compression mode   -a = disable attributes")
	fmt.Println("  -r = Include subdirectories")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}

	cmd := strings.ToLower(os.Args[1])
	if cmd == "h" {
		usage()
		return
	}

	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	optY := fs.Bool("y", false, "Assume YES on all queries")
	optF := fs.Bool("f", false, "Set fast compression mode")
	optM := fs.Bool("m", false, "Set maximum compression mode")
	optA := fs.Bool("a", false, "Disable attributes")
	optR := fs.Bool("r", false, "Include subdirectories")

	// Parse flags starting from the 3rd argument (after the executable and the command)
	fs.Parse(os.Args[2:])
	args := fs.Args()

	if len(args) == 0 {
		usage()
		return
	}

	archiveName := args[0]
	if !strings.Contains(archiveName, ".") {
		archiveName += ".asd"
	}

	var files []string
	if len(args) > 1 {
		files = args[1:]
	}

	opts := Options{
		AssumeYes: *optY,
		Fast:      *optF,
		Max:       *optM,
		NoAttrs:   *optA,
		Recursive: *optR,
	}

	switch cmd {
	case "a":
		addArchive(archiveName, files, opts)
	case "l":
		listArchive(archiveName)
	case "x":
		extractArchive(archiveName, false)
	case "t":
		extractArchive(archiveName, true)
	default:
		fmt.Printf("Option '%s' not yet fully implemented in this port.\n", cmd)
	}
}

func addArchive(name string, files []string, opts Options) {
	hashDeep := 300 // Default
	asdExtra := 20  // Default

	if opts.Fast {
		hashDeep = 20
	} else if opts.Max {
		hashDeep = 4095
	}

	var entries []archive.FileEntry
	var fullPaths []string

	addFile := func(path string, info os.FileInfo) error {
		if info.IsDir() {
			return nil
		}
		fmt.Printf("Adding: %s\n", path)
		crc, err := calculateCRC(path)
		if err != nil {
			return err
		}
		
		archiveName := filepath.ToSlash(path)
		entries = append(entries, archive.FileEntry{
			Name:      archiveName,
			Size:      uint32(info.Size()),
			CRC:       crc,
			Time:      0,
			Attribute: 32,
		})
		fullPaths = append(fullPaths, path)
		return nil
	}

	for _, pattern := range files {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			fmt.Printf("Error with pattern %s: %v\n", pattern, err)
			continue
		}
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil {
				continue
			}

			if info.IsDir() && opts.Recursive {
				filepath.Walk(m, func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					return addFile(path, info)
				})
			} else {
				addFile(m, info)
			}
		}
	}

	if len(entries) == 0 {
		fmt.Println("No files found to add.")
		return
	}

	out, err := os.Create(name)
	if err != nil {
		fmt.Printf("Error creating archive: %v\n", err)
		return
	}
	defer out.Close()

	header := &archive.ArchiveHeader{Files: entries, Version: 1}

	if err := header.WriteHeader(out); err != nil {
		fmt.Printf("Error writing header: %v\n", err)
		return
	}

	tmp, err := os.CreateTemp("", "goasd-solid")
	if err != nil {
		fmt.Printf("Error creating temp file: %v\n", err)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	for _, p := range fullPaths {
		f, err := os.Open(p)
		if err != nil {
			fmt.Printf("Error reading file %s: %v\n", p, err)
			return
		}
		io.Copy(tmp, f)
		f.Close()
	}

	tmp.Seek(0, 0)
	fmt.Printf("Compressing %d files...\n", len(entries))

	if err := archive.Compress(out, tmp, asdExtra, hashDeep); err != nil {
		fmt.Printf("Error during compression: %v\n", err)
		return
	}

	fmt.Println("Archive created successfully.")
}

func calculateCRC(name string) (uint32, error) {
	f, err := os.Open(name)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	h := crc32.NewIEEE()
	if _, err := io.Copy(h, f); err != nil {
		return 0, err
	}
	return h.Sum32(), nil
}

func listArchive(name string) {
	f, err := os.Open(name)
	if err != nil {
		fmt.Printf("Error opening archive: %v\n", err)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		fmt.Printf("Error statting archive: %v\n", err)
		return
	}
	archiveSize := info.Size()

	header, err := archive.ReadHeader(f)
	if err != nil {
		fmt.Printf("Error reading header: %v\n", err)
		return
	}

	fmt.Printf("Listing archive: [%s] (Version: ASD0%d)\n\n", name, header.Version)
	fmt.Printf("Filename            filesize     Crc32     Date       Time       Attribute\n")
	fmt.Println(strings.Repeat("-", 75))

	var totalSize uint64
	for _, file := range header.Files {
		totalSize += uint64(file.Size)

		ttime := file.Time & 0xFFFF
		ddate := file.Time >> 16

		year := ((ddate & 0xfe00) >> 9) + 1980
		month := (ddate & 0x01e0) >> 5
		day := ddate & 0x001f
		hour := (ttime & 0xf800) >> 11
		minute := (ttime & 0x07e0) >> 5
		second := (ttime & 0x001f) << 1

		attr := []byte("- - - -  ")
		if file.Attribute&32 != 0 { // FA_ARCH
			attr[0] = 'N'
		}
		if file.Attribute&1 != 0 { // FA_RDONLY
			attr[2] = 'R'
		}
		if file.Attribute&2 != 0 { // FA_HIDDEN
			attr[4] = 'H'
		}
		if file.Attribute&4 != 0 { // FA_SYSTEM
			attr[6] = 'S'
		}

		fmt.Printf("%-18s %11d  %08X  %04d-%02d-%02d %02d:%02d:%02d   %s\n",
			file.Name, file.Size, file.CRC,
			year, month, day, hour, minute, second,
			string(attr))
	}
	fmt.Println(strings.Repeat("-", 75))

	ratio := uint64(0)
	if totalSize > 0 {
		ratio = 100 - ((uint64(archiveSize) * 100) / totalSize)
	}
	fmt.Printf("Total file(s) in archive:%d, Space saved %d%%\n", len(header.Files), ratio)
}

func extractArchive(name string, testOnly bool) {
	f, err := os.Open(name)
	if err != nil {
		fmt.Printf("Error opening archive: %v\n", err)
		return
	}
	defer f.Close()

	header, err := archive.ReadHeader(f)
	if err != nil {
		fmt.Printf("Error reading header: %v\n", err)
		return
	}

	if testOnly {
		fmt.Printf("Testing archive: [%s] (Version: ASD0%d)\n", name, header.Version)
	} else {
		fmt.Printf("Extracting archive: [%s] (Version: ASD0%d)\n", name, header.Version)
	}

	err = header.Decompress(f, ".", testOnly)
	if err != nil {
		fmt.Printf("\nError during operation: %v\n", err)
	} else {
		fmt.Println("\nAll files OK!")
	}
}
