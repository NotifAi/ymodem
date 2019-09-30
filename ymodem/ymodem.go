package ymodem

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	ytypes "github.com/NotifAi/ymodem/types"
)

const SOH byte = 0x01
const STX byte = 0x02
const EOT byte = 0x04
const ACK byte = 0x06
const NAK byte = 0x15
const CAN byte = 0x18
const POLL byte = 0x43

var InvalidPacket = errors.New("invalid packet")

type File struct {
	Data     []byte
	Name     string
	blocks   int
	bytesBar ytypes.Bar
}

func CRC16(data []byte) uint16 {
	var u16CRC uint16 = 0

	for _, character := range data {
		part := uint16(character)

		u16CRC = u16CRC ^ (part << 8)
		for i := 0; i < 8; i++ {
			if u16CRC&0x8000 > 0 {
				u16CRC = u16CRC<<1 ^ 0x1021
			} else {
				u16CRC = u16CRC << 1
			}
		}
	}

	return u16CRC
}

func CRC16Constant(data []byte, length int) uint16 {
	var u16CRC uint16 = 0

	for _, character := range data {
		part := uint16(character)

		u16CRC = u16CRC ^ (part << 8)
		for i := 0; i < 8; i++ {
			if u16CRC&0x8000 > 0 {
				u16CRC = u16CRC<<1 ^ 0x1021
			} else {
				u16CRC = u16CRC << 1
			}
		}
	}

	for c := 0; c < length-len(data); c++ {
		u16CRC = u16CRC ^ (0x04 << 8)
		for i := 0; i < 8; i++ {
			if u16CRC&0x8000 > 0 {
				u16CRC = u16CRC<<1 ^ 0x1021
			} else {
				u16CRC = u16CRC << 1
			}
		}
	}

	return u16CRC
}

func sendBlock(c io.ReadWriter, bs int, block uint8, data []byte) error {
	// send STX
	if _, err := c.Write([]byte{SOH}); err != nil {
		return err
	}
	if _, err := c.Write([]byte{block}); err != nil {
		return err
	}
	if _, err := c.Write([]byte{255 - block}); err != nil {
		return err
	}

	// send data
	var toSend bytes.Buffer
	toSend.Write(data)

	padding := bs - len(data)
	if padding > 0 {
		buf := make([]byte, padding)
		toSend.Write(buf)
	}

	// calc CRC
	u16CRC := CRC16Constant(data, bs)
	toSend.Write([]byte{uint8(u16CRC >> 8)})
	toSend.Write([]byte{uint8(u16CRC & 0x0FF)})

	sent := 0
	for sent < toSend.Len() {
		if n, err := c.Write(toSend.Bytes()[sent:]); err != nil {
			return err
		} else {
			sent += n
		}
	}

	return nil
}

func ModemSend(c io.ReadWriter, progress ytypes.Progress, bs int, files []File) error {
	oBuffer := make([]byte, 1)

	if progress == nil {
		progress = ytypes.DummyProgress()
	}

	cancel := func() {
		_, _ = c.Write([]byte{CAN, CAN})
	}

	var err error

	defer func() {
		if err != nil {
			cancel()
		}
	}()

	for fi := range files {
		var blocks = len(files[fi].Data) / bs
		if len(files[fi].Data) > (blocks * bs) {
			blocks++
		}

		blocks++

		files[fi].blocks = blocks
		files[fi].bytesBar = progress.Create(files[fi].Name, len(files[fi].Data))
	}

	retryCount := 5

	min := func(x, y int) int {
		if x <= y {
			return x
		}

		return y
	}

	for fi := range files {
		// Wait for Poll
		if _, err = c.Read(oBuffer); err != nil {
			return err
		}

		if oBuffer[0] == POLL {
			for i := 0; i < 5; i++ {
				var send bytes.Buffer
				send.WriteString(files[fi].Name)
				send.WriteByte(0x0)
				send.WriteString(fmt.Sprintf("%d ", len(files[fi].Data)))
				for send.Len() < bs {
					send.Write([]byte{0x0})
				}

				if err = sendBlock(c, bs, 0, send.Bytes()); err != nil {
					return err
				}

				// Wait for ACK
				if _, err = c.Read(oBuffer); err != nil {
					return err
				}

				switch oBuffer[0] {
				case NAK:
					retryCount--
					if retryCount == 0 {
						err = errors.New("amount of retries exceeded")
						return err
					}
				case ACK:
					goto confirmation
				default:
					err = errors.New("failed to send initial block")
					return err
				}
			}
		} else {
			err = errors.New("invalid handshake symbol")
			return err
		}

	confirmation:
		// Wait for Poll
		if _, err = c.Read(oBuffer); err != nil {
			return err
		}

		// Send file data
		if oBuffer[0] == POLL {
			failed := 0
			var block = 1
			for block < files[fi].blocks && failed < 10 {
				from := (block - 1) * bs
				remaining := len(files[fi].Data[from:])

				to := min(remaining, bs) + from

				if err = sendBlock(c, bs, uint8(block), files[fi].Data[from:to]); err != nil {
					return err
				}

				if _, err := c.Read(oBuffer); err != nil {
					return err
				}

				if oBuffer[0] == ACK {
					block++
					_ = files[fi].bytesBar.Add(to - from)
				} else {
					failed++
				}
			}
		}

		// Wait for ACK and send EOT
		if _, err = c.Write([]byte{EOT}); err != nil {
			return err
		}

		if _, err = c.Read(oBuffer); err != nil {
			return err
		}

		if oBuffer[0] != ACK {
			return fmt.Errorf("eot stage 1: expected NAK. received %02X", oBuffer[0])
		}
	}

	// Wait for POLL
	if _, err = c.Read(oBuffer); err != nil {
		return err
	}

	if oBuffer[0] != POLL {
		return errors.New("eot stage 3: failed to send end block")
	}

	// Send empty block to signify end
	var zero bytes.Buffer
	zero.Write(make([]byte, bs))

	if err := sendBlock(c, bs, 0, zero.Bytes()); err != nil {
		return err
	}

	// Wait for ACK
	if _, err := c.Read(oBuffer); err != nil {
		return err
	}

	if oBuffer[0] != ACK {
		return errors.New("stage 4: failed to send end block")
	}

	return nil
}

func receivePacket(c io.ReadWriter, bs int) ([]byte, error) {
	oBuffer := make([]byte, 1)
	dBuffer := make([]byte, bs)

	if _, err := c.Read(oBuffer); err != nil {
		return nil, err
	}
	pType := oBuffer[0]

	if pType == EOT {
		return nil, nil
	}

	var packetSize int
	switch pType {
	case SOH:
		packetSize = bs
		break
	case STX:
		packetSize = bs
		break
	}

	if _, err := c.Read(oBuffer); err != nil {
		return nil, err
	}
	packetCount := oBuffer[0]

	if _, err := c.Read(oBuffer); err != nil {
		return nil, err
	}
	inverseCount := oBuffer[0]

	if packetCount > inverseCount || inverseCount+packetCount != 255 {
		if _, err := c.Write([]byte{NAK}); err != nil {
			return nil, err
		}
		return nil, InvalidPacket
	}

	received := 0
	var pData bytes.Buffer
	for received < packetSize {
		n, err := c.Read(dBuffer)
		if err != nil {
			return nil, err
		}

		received += n
		pData.Write(dBuffer[:n])
	}

	var crc uint16
	if _, err := c.Read(oBuffer); err != nil {
		return nil, err
	}
	crc = uint16(oBuffer[0])

	if _, err := c.Read(oBuffer); err != nil {
		return nil, err
	}
	crc <<= 8
	crc |= uint16(oBuffer[0])

	// Calculate CRC
	crcCalc := CRC16(pData.Bytes())
	if crcCalc != crc {
		if _, err := c.Write([]byte{NAK}); err != nil {
			return nil, err
		}
	}

	if _, err := c.Write([]byte{ACK}); err != nil {
		return nil, err
	}

	return pData.Bytes(), nil
}

func ModemReceive(c io.ReadWriter, bs int) (string, []byte, error) {
	var data bytes.Buffer

	// Start Connection
	if _, err := c.Write([]byte{POLL}); err != nil {
		return "", nil, err
	}

	// Read file information
	pktData, err := receivePacket(c, bs)
	if err != nil {
		return "", nil, err
	}

	filenameEnd := bytes.IndexByte(pktData, 0x0)
	filename := string(pktData[0:filenameEnd])

	var filesize int
	if _, err := fmt.Sscanf(string(pktData[filenameEnd+1:]), "%d", &filesize); err != nil {
		return "", nil, err
	}

	if _, err := c.Write([]byte{POLL}); err != nil {
		return "", nil, err
	}

	// Read Packets
	for {
		pktData, err := receivePacket(c, bs)
		if err == InvalidPacket {
			continue
		}

		if err != nil {
			return "", nil, err
		}

		// End of Transmission
		if pktData == nil {
			break
		}

		data.Write(pktData)
	}

	// Send NAK to respond to EOT
	if _, err := c.Write([]byte{NAK}); err != nil {
		return "", nil, err
	}

	oBuffer := make([]byte, 1)
	if _, err := c.Read(oBuffer); err != nil {
		return "", nil, err
	}

	// Send ACK to respond to second EOT
	if oBuffer[0] != EOT {
		return "", nil, err
	}

	if _, err := c.Write([]byte{ACK}); err != nil {
		return "", nil, err
	}

	// Second POLL to get remaining file or close
	if _, err := c.Write([]byte{POLL}); err != nil {
		return "", nil, err
	}

	// Get remaining data ( for now assume one file )
	if _, err := receivePacket(c, bs); err != nil {
		return "", nil, err
	}

	return filename, data.Bytes()[0:filesize], nil
}
