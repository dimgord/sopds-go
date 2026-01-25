package scanner

import (
	"encoding/binary"
	"io"
	"os"
	"strings"
	"time"
)

// GetAudioDuration returns the actual duration of an audio file
func GetAudioDuration(path string) (time.Duration, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return 0, err
	}

	ext := strings.ToLower(path[strings.LastIndex(path, ".")+1:])
	return GetAudioDurationFromReader(f, stat.Size(), ext)
}

// GetAudioDurationFromReader returns actual duration from a reader
func GetAudioDurationFromReader(r io.ReadSeeker, size int64, format string) (time.Duration, error) {
	switch format {
	case "m4b", "m4a", "m4p", "mp4", "aac":
		return getMP4Duration(r, size)
	case "mp3":
		return getMP3Duration(r, size)
	case "flac":
		return getFLACDuration(r)
	case "ogg", "opus":
		return getOggDuration(r, size)
	default:
		return 0, nil
	}
}

// getMP4Duration extracts duration from MP4/M4B/M4A files
// Duration is stored in mvhd (movie header) atom
func getMP4Duration(r io.ReadSeeker, fileSize int64) (time.Duration, error) {
	// Search for moov atom, then mvhd inside it
	var pos int64 = 0

	for pos < fileSize {
		r.Seek(pos, io.SeekStart)

		// Read atom header (8 bytes: 4 size + 4 type)
		header := make([]byte, 8)
		if _, err := io.ReadFull(r, header); err != nil {
			return 0, err
		}

		atomSize := int64(binary.BigEndian.Uint32(header[0:4]))
		atomType := string(header[4:8])

		// Handle extended size (size == 1 means 64-bit size follows)
		if atomSize == 1 {
			extSize := make([]byte, 8)
			if _, err := io.ReadFull(r, extSize); err != nil {
				return 0, err
			}
			atomSize = int64(binary.BigEndian.Uint64(extSize))
		}

		if atomSize == 0 {
			// Atom extends to end of file
			atomSize = fileSize - pos
		}

		if atomType == "moov" {
			// Search for mvhd inside moov
			return findMvhdInMoov(r, pos+8, atomSize-8)
		}

		pos += atomSize
	}

	return 0, nil
}

// findMvhdInMoov searches for mvhd atom within moov
func findMvhdInMoov(r io.ReadSeeker, start, size int64) (time.Duration, error) {
	pos := start
	end := start + size

	for pos < end {
		r.Seek(pos, io.SeekStart)

		header := make([]byte, 8)
		if _, err := io.ReadFull(r, header); err != nil {
			return 0, err
		}

		atomSize := int64(binary.BigEndian.Uint32(header[0:4]))
		atomType := string(header[4:8])

		if atomSize == 1 {
			extSize := make([]byte, 8)
			if _, err := io.ReadFull(r, extSize); err != nil {
				return 0, err
			}
			atomSize = int64(binary.BigEndian.Uint64(extSize))
		}

		if atomType == "mvhd" {
			return parseMvhd(r)
		}

		pos += atomSize
	}

	return 0, nil
}

// parseMvhd parses the movie header atom for duration
func parseMvhd(r io.ReadSeeker) (time.Duration, error) {
	// Read version byte
	versionByte := make([]byte, 1)
	if _, err := io.ReadFull(r, versionByte); err != nil {
		return 0, err
	}
	version := versionByte[0]

	// Skip flags (3 bytes)
	r.Seek(3, io.SeekCurrent)

	var timescale uint32
	var duration uint64

	if version == 0 {
		// 32-bit version
		// Skip creation_time (4) + modification_time (4)
		r.Seek(8, io.SeekCurrent)

		// Read timescale (4 bytes)
		tsBytes := make([]byte, 4)
		if _, err := io.ReadFull(r, tsBytes); err != nil {
			return 0, err
		}
		timescale = binary.BigEndian.Uint32(tsBytes)

		// Read duration (4 bytes)
		durBytes := make([]byte, 4)
		if _, err := io.ReadFull(r, durBytes); err != nil {
			return 0, err
		}
		duration = uint64(binary.BigEndian.Uint32(durBytes))
	} else {
		// 64-bit version
		// Skip creation_time (8) + modification_time (8)
		r.Seek(16, io.SeekCurrent)

		// Read timescale (4 bytes)
		tsBytes := make([]byte, 4)
		if _, err := io.ReadFull(r, tsBytes); err != nil {
			return 0, err
		}
		timescale = binary.BigEndian.Uint32(tsBytes)

		// Read duration (8 bytes)
		durBytes := make([]byte, 8)
		if _, err := io.ReadFull(r, durBytes); err != nil {
			return 0, err
		}
		duration = binary.BigEndian.Uint64(durBytes)
	}

	if timescale == 0 {
		return 0, nil
	}

	seconds := float64(duration) / float64(timescale)
	return time.Duration(seconds * float64(time.Second)), nil
}

// getMP3Duration extracts duration from MP3 files
// Tries Xing/VBRI header first, then estimates from frame count
func getMP3Duration(r io.ReadSeeker, fileSize int64) (time.Duration, error) {
	// Find first frame and check for Xing/VBRI header
	r.Seek(0, io.SeekStart)

	// Skip ID3v2 tag if present
	id3Header := make([]byte, 10)
	if _, err := io.ReadFull(r, id3Header); err != nil {
		return 0, err
	}

	var dataStart int64 = 0
	if string(id3Header[0:3]) == "ID3" {
		// ID3v2 tag present, calculate size
		tagSize := int64(id3Header[6]&0x7f)<<21 |
			int64(id3Header[7]&0x7f)<<14 |
			int64(id3Header[8]&0x7f)<<7 |
			int64(id3Header[9]&0x7f)
		dataStart = 10 + tagSize
	}

	r.Seek(dataStart, io.SeekStart)

	// Find sync word (0xFF 0xFB/FA/F3/F2)
	buf := make([]byte, 4)
	for {
		if _, err := io.ReadFull(r, buf); err != nil {
			return 0, err
		}

		if buf[0] == 0xFF && (buf[1]&0xE0) == 0xE0 {
			// Found frame sync
			break
		}

		// Move back 3 bytes to not miss sync
		r.Seek(-3, io.SeekCurrent)
	}

	// Parse frame header
	version := (buf[1] >> 3) & 0x03
	layer := (buf[1] >> 1) & 0x03
	bitrateIndex := (buf[2] >> 4) & 0x0F
	sampleRateIndex := (buf[2] >> 2) & 0x03

	// Get sample rate
	sampleRates := map[byte]map[byte]int{
		3: {0: 44100, 1: 48000, 2: 32000}, // MPEG1
		2: {0: 22050, 1: 24000, 2: 16000}, // MPEG2
		0: {0: 11025, 1: 12000, 2: 8000},  // MPEG2.5
	}

	sampleRate := 44100
	if rates, ok := sampleRates[version]; ok {
		if rate, ok := rates[sampleRateIndex]; ok {
			sampleRate = rate
		}
	}

	// Get samples per frame
	samplesPerFrame := 1152 // Default for Layer 3
	if layer == 3 {         // Layer 1
		samplesPerFrame = 384
	} else if version != 3 && layer == 1 { // Layer 3 MPEG2/2.5
		samplesPerFrame = 576
	}

	// Look for Xing header (36 bytes after frame header for stereo)
	xingOffset := int64(36)
	if (buf[3]>>6)&0x03 == 3 { // Mono
		xingOffset = 21
	}

	framePos, _ := r.Seek(0, io.SeekCurrent)
	r.Seek(framePos-4+xingOffset, io.SeekStart)

	xingHeader := make([]byte, 8)
	if _, err := io.ReadFull(r, xingHeader); err == nil {
		tag := string(xingHeader[0:4])
		if tag == "Xing" || tag == "Info" {
			flags := binary.BigEndian.Uint32(xingHeader[4:8])
			if flags&0x01 != 0 { // Frames field present
				framesBytes := make([]byte, 4)
				if _, err := io.ReadFull(r, framesBytes); err == nil {
					totalFrames := binary.BigEndian.Uint32(framesBytes)
					totalSamples := int64(totalFrames) * int64(samplesPerFrame)
					seconds := float64(totalSamples) / float64(sampleRate)
					return time.Duration(seconds * float64(time.Second)), nil
				}
			}
		}
	}

	// No Xing header - estimate from bitrate
	bitrates := map[byte]map[byte]int{
		// MPEG1 Layer 3
		3: {1: 32, 2: 40, 3: 48, 4: 56, 5: 64, 6: 80, 7: 96, 8: 112, 9: 128, 10: 160, 11: 192, 12: 224, 13: 256, 14: 320},
	}

	bitrate := 128
	if rates, ok := bitrates[version]; ok {
		if rate, ok := rates[bitrateIndex]; ok {
			bitrate = rate
		}
	}

	// Estimate: duration = (fileSize * 8) / (bitrate * 1000)
	audioSize := fileSize - dataStart
	seconds := float64(audioSize*8) / float64(bitrate*1000)
	return time.Duration(seconds * float64(time.Second)), nil
}

// getFLACDuration extracts duration from FLAC files
func getFLACDuration(r io.ReadSeeker) (time.Duration, error) {
	// Check fLaC magic
	magic := make([]byte, 4)
	if _, err := io.ReadFull(r, magic); err != nil {
		return 0, err
	}
	if string(magic) != "fLaC" {
		return 0, nil
	}

	// Read STREAMINFO metadata block
	for {
		blockHeader := make([]byte, 4)
		if _, err := io.ReadFull(r, blockHeader); err != nil {
			return 0, err
		}

		isLast := (blockHeader[0] & 0x80) != 0
		blockType := blockHeader[0] & 0x7F
		blockLength := int(blockHeader[1])<<16 | int(blockHeader[2])<<8 | int(blockHeader[3])

		if blockType == 0 { // STREAMINFO
			streamInfo := make([]byte, blockLength)
			if _, err := io.ReadFull(r, streamInfo); err != nil {
				return 0, err
			}

			// Sample rate: bits 80-99 (20 bits)
			sampleRate := uint32(streamInfo[10])<<12 | uint32(streamInfo[11])<<4 | uint32(streamInfo[12]>>4)

			// Total samples: bits 108-143 (36 bits)
			totalSamples := uint64(streamInfo[13]&0x0F)<<32 |
				uint64(streamInfo[14])<<24 |
				uint64(streamInfo[15])<<16 |
				uint64(streamInfo[16])<<8 |
				uint64(streamInfo[17])

			if sampleRate > 0 {
				seconds := float64(totalSamples) / float64(sampleRate)
				return time.Duration(seconds * float64(time.Second)), nil
			}
			return 0, nil
		}

		// Skip to next block
		r.Seek(int64(blockLength), io.SeekCurrent)

		if isLast {
			break
		}
	}

	return 0, nil
}

// getOggDuration extracts duration from OGG files (Vorbis/Opus)
func getOggDuration(r io.ReadSeeker, fileSize int64) (time.Duration, error) {
	// For OGG, we need to find the last granule position
	// This is complex, so we'll use a simpler approach:
	// Read the header to get sample rate, then seek to end for last granule

	// Check OggS magic
	magic := make([]byte, 4)
	if _, err := io.ReadFull(r, magic); err != nil {
		return 0, err
	}
	if string(magic) != "OggS" {
		return 0, nil
	}

	// Skip to segment table to find vorbis/opus header
	r.Seek(26, io.SeekStart) // Skip to segment count
	segCountByte := make([]byte, 1)
	if _, err := io.ReadFull(r, segCountByte); err != nil {
		return 0, err
	}
	segCount := int(segCountByte[0])

	// Skip segment table
	r.Seek(int64(segCount), io.SeekCurrent)

	// Read codec header
	codecHeader := make([]byte, 8)
	if _, err := io.ReadFull(r, codecHeader); err != nil {
		return 0, err
	}

	var sampleRate uint32

	if codecHeader[0] == 0x01 && string(codecHeader[1:7]) == "vorbis" {
		// Vorbis identification header layout:
		// - byte 0: packet_type (0x01)
		// - bytes 1-6: "vorbis"
		// - bytes 7-10: version (4 bytes)
		// - byte 11: channels (1 byte)
		// - bytes 12-15: sample_rate (4 bytes)
		// We've read 8 bytes (codecHeader), so we're at offset 8
		// Need to skip 3 more bytes of version + 1 byte channels = 4 bytes
		r.Seek(4, io.SeekCurrent) // Skip remaining version (3 bytes) + channels (1 byte)
		srBytes := make([]byte, 4)
		if _, err := io.ReadFull(r, srBytes); err != nil {
			return 0, err
		}
		sampleRate = binary.LittleEndian.Uint32(srBytes)
	} else if string(codecHeader[0:8]) == "OpusHead" {
		// Opus always uses 48000 Hz internally
		sampleRate = 48000
	} else {
		return 0, nil
	}

	if sampleRate == 0 {
		return 0, nil
	}

	// Seek near end to find last OggS page and read granule position
	// We look in the last 64KB
	searchSize := int64(65536)
	if searchSize > fileSize {
		searchSize = fileSize
	}

	r.Seek(fileSize-searchSize, io.SeekStart)
	buf := make([]byte, searchSize)
	n, _ := r.Read(buf)

	// Find last OggS marker
	var lastGranule uint64
	for i := n - 14; i >= 0; i-- {
		if buf[i] == 'O' && buf[i+1] == 'g' && buf[i+2] == 'g' && buf[i+3] == 'S' {
			// Found OggS, granule position is at offset 6 (8 bytes, little endian)
			lastGranule = binary.LittleEndian.Uint64(buf[i+6 : i+14])
			break
		}
	}

	if lastGranule > 0 {
		seconds := float64(lastGranule) / float64(sampleRate)
		return time.Duration(seconds * float64(time.Second)), nil
	}

	return 0, nil
}
