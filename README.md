# goasd
Golang port of ASD Archiver version 0.1.5 (1997) with 0.2.0 Decompression Support

### Features
- **Compression (V1)**: Uses a sliding window (LZ77-style) compression algorithm with a 4KB buffer (`ASD01` format).
- **Decompression (V1 & V2)**: Seamlessly decompresses both original 0.1.5 (`ASD01`) archives and 0.2.0 (`ASD02`) archives. V2 extraction features a fully reverse-engineered implementation of Tobias Svensson's bespoke 1998 Adaptive Huffman + LZSS algorithm.
- **Integrity**: CRC32 checksums for every file in the archive to ensure data integrity.
- **Metadata**: Stores original filenames, file sizes, timestamps, and file attributes.
- **Variable Compression**: Supports Fast, Normal, and Maximum compression levels via hash table depth adjustment (for V1).
- **Subdirectories**: Supports recursive directory archiving via the `-r` flag.
- **Compatibility**: Designed to handle long filenames (standard for late 90s systems).

*(Note: While `goasd` can decompress 0.2.0 archives perfectly, creating new archives is currently restricted to the 0.1.5 `ASD01` format.)*

### Command Line Usage
`goasd <option> [<switch(es)>] <archive_name> [<files...>]`

#### Options
- `a`: Add files to archive.
- `x`: Extract files from archive.
- `l`: List files in archive.
- `t`: Test files in archive (perform CRC check without extracting).
- `h`: Show help and usage examples.

#### Switches
- `-y`: Assume "Yes" on all queries (e.g., overwrite existing files).
- `-f`: Fast compression mode (lower ratio, higher speed).
- `-m`: Maximum compression mode (highest ratio, lower speed).
- `-a`: Disable/ignore file attributes during compression or extraction.
- `-r`: Include subdirectories (recursive).

### Archive Format Layout (V1 - ASD01)
1. **Signature**: `ASD01` followed by `0x1A` (6 bytes).
2. **File Count**: 16-bit little-endian integer (2 bytes).
3. **File Headers Loop**:
   - Filename length (1 byte)
   - Filename string
   - File size (4 bytes, little-endian)
   - CRC32 (4 bytes)
   - File Time/Date (4 bytes, MS-DOS format)
   - Attributes (2 bytes)
4. **Extra Data**: 1 byte (compression parameter).
5. **Compressed Data**: The bitstream of compressed file data (LZSS).

### Archive Format Layout (V2 - ASD02 Differences)
- **Signature**: `ASD02\x1A`.
- **File Count**: 24-bit integer (2 bytes little-endian, plus 1 additional byte containing the high 8 bits).
- **Configuration Header**: The single `Extra Data` byte is replaced by a sequence of 6 bit-fields defining the window size, buffer size, and the algorithmic rescaling/rebuilding factors for the dual Adaptive Huffman trees.
- **Compressed Data**: LZSS + Adaptive Huffman encoded bitstream.
