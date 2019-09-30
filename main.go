package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/NotifAi/serial"
	"github.com/spf13/cobra"

	"github.com/NotifAi/ymodem/ymodem"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	var port string
	var message string
	var wait string
	var blockSize string
	var bSize = 128

	var cmdSend = &cobra.Command{
		Use:   "send [port]",
		Short: "Send file",
		Long:  ``,
		Run: func(cmd *cobra.Command, args []string) {
			// Open connection
			connection, err := serial.OpenPort(&serial.Config{Name: port, Baud: 115200})
			if err != nil {
				log.Fatalln(err)
			}

			// Send initial message
			if len(message) > 0 {
				if _, err := connection.Write([]byte(message + "\n")); err != nil {
					log.Println(err)
				}
			}

			time.Sleep(2 * time.Second)

			// Wait for message
			if len(wait) > 0 {
				var result string
				buffer := make([]byte, 64)

				for {
					n, err := connection.Read(buffer)
					if err != nil {
						log.Fatalln(err)
					}
					if n == 0 {
						break
					}
					result = string(buffer[0:n])
					log.Println(result)
					if strings.Contains(result, wait) {
						break
					}
				}
			}

			var files []ymodem.File

			for _, f := range args {
				// Open file
				fIn, err := os.Open(f)
				if err != nil {
					log.Fatalln(err)
				}

				data, err := ioutil.ReadAll(fIn)
				if err != nil {
					log.Fatalln(err)
				}

				_ = fIn.Close()

				files = append(files, ymodem.File{Data: data, Name: filepath.Base(f)})
			}

			bar := newProgress()
			// Send files
			err = ymodem.ModemSend(connection, bar, bSize, files)

			bar.Shutdown()

			if err != nil {
				log.Fatalln(err.Error())
			}

			fmt.Println("sent successfully")
		},
	}
	cmdSend.Flags().StringVarP(&port, "port", "p", "", "serial port to connect to")
	cmdSend.Flags().StringVarP(&message, "message", "m", "", "message to initiate data transfer")
	cmdSend.Flags().StringVarP(&wait, "wait", "w", "", "message to wait before initiating data transfer")
	cmdSend.Flags().StringVarP(&blockSize, "block_size", "b", "128", "size of transfer 128|1024")

	var cmdReceive = &cobra.Command{
		Use:   "receive [port]",
		Short: "Receive file",
		Long:  ``,
		Run: func(cmd *cobra.Command, args []string) {
			// Open connection
			connection, err := serial.OpenPort(&serial.Config{Name: port, Baud: 115200})
			if err != nil {
				log.Fatalln(err)
			}

			// Send initial message
			if len(message) > 0 {
				if _, err := connection.Write([]byte(message + "\r\n")); err != nil {
					log.Println(err)
				}
			}

			// Wait for message
			if len(wait) > 0 {
				var result string
				buffer := make([]byte, 64)
				for {
					n, err := connection.Read(buffer)
					if err != nil {
						log.Fatalln(err)
					}
					if n == 0 {
						break
					}
					result += string(buffer[0:n])
					if strings.Contains(result, wait) {
						break
					}
				}
			}

			// Receive file
			filename, data, err := ymodem.ModemReceive(connection, bSize)
			if err != nil {
				log.Fatalln(err)
			}

			// Write file
			fOut, err := os.Create(filename)
			if err != nil {
				log.Fatalln(err)
			}
			_, _ = fOut.Write(data)
			_ = fOut.Close()

			log.Println(filename, "write successful")
		},
	}
	cmdReceive.Flags().StringVarP(&port, "port", "p", "", "serial port to connect to")
	cmdReceive.Flags().StringVarP(&message, "message", "m", "", "message to initiate data transfer")
	cmdReceive.Flags().StringVarP(&wait, "wait", "w", "", "message to wait before initiating data transfer")
	cmdReceive.Flags().StringVarP(&blockSize, "block_size", "b", "128", "size of transfer 128|1024")

	var Root = &cobra.Command{
		Use:   "ymodem",
		Short: "",
		Long:  ``,
		Run: func(cmd *cobra.Command, args []string) {
			if blockSize == "" {
				bSize = 128
			} else {
				v, e := strconv.ParseInt(blockSize, 10, 32)
				if e != nil || (v != 128 && v != 1024) {
					log.Fatalln("invalid block size value")
				}

				bSize = int(v)
			}
		},
	}

	Root.AddCommand(cmdSend)
	Root.AddCommand(cmdReceive)
	_ = Root.Execute()
}
