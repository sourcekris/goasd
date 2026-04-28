# goasd
Golang port of ASD Archiver version 0.1.5 (1997)

### Features
- **Compression**: Uses a sliding window (LZ77-style) compression algorithm with a 4KB buffer.
- **Integrity**: CRC32 checksums for every file in the archive to ensure data integrity.
- **Metadata**: Stores original filenames, file sizes, timestamps, and file attributes.
- **Variable Compression**: Supports Fast, Normal, and Maximum compression levels via hash table depth adjustment.
- **Compatibility**: Designed to handle long filenames (standard for late 90s systems).

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
- `-r`: Recursive subdirectory support (noted in original source).

### Archive Format Layout
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
5. **Compressed Data**: The bitstream of compressed file data.
