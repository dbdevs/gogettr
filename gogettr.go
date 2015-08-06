package main

import (
	"bytes"

	"golang.org/x/crypto/ssh"

	"fmt"

	"github.com/codegangsta/cli"
	"github.com/dustin/go-humanize"
	"github.com/wsxiaoys/terminal/color"
	"io/ioutil"
	"log"
	"os"
	"os/user"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var config *ssh.ClientConfig

type Result struct {
	Host string
	Res  string
}

func strip(v string) string {
	return strings.TrimSpace(strings.Trim(v, "\n"))
}

func filterHosts(hosts []string) []string {
	var res []string
	for _, host := range hosts {
		var conn string
		token := strings.Split(host, ":")
		if len(token) == 1 {
			conn = host + ":22"
		} else {
			conn = host
		}
		res = append(res, conn)
	}
	return res
}

func getKeyFile() (key ssh.Signer, err error) {
	usr, _ := user.Current()
	file := usr.HomeDir + "/.ssh/id_rsa"
	buf, err := ioutil.ReadFile(file)
	if err != nil {
		return
	}
	key, err = ssh.ParsePrivateKey(buf)
	if err != nil {
		return
	}
	return
}

func main() {
	key, err := getKeyFile()
	if err != nil {
		panic(err)
	}
	usr, _ := user.Current()
	config = &ssh.ClientConfig{
		User: usr.Username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(key),
		},
	}

	app := cli.NewApp()
	app.Name = "Docker Ruby Manager"
	app.Usage = "Manage your ruby versions with Docker"
	app.Version = "0.1.0"
	app.Author = "Daniel Barker"
	app.EnableBashCompletion = true

	app.Commands = []cli.Command{
		{
			Name:   "sshcmd",
			Usage:  "Run any cmd through ssh",
			Action: sshcmd,
			Flags: []cli.Flag{
				cli.StringSliceFlag{
					Name:  "node, n",
					Usage: "A node to run the cmd on. Use multiple times",
				},
				cli.StringFlag{
					Name:  "nodes",
					Usage: "A comma separated list of nodes for the cmd",
				},
				cli.StringFlag{
					Name:  "cmd, c",
					Usage: "The cmd to run",
				},
			},
		},
		{
			Name:   "largestWithNodes",
			Usage:  "Get a listing of largest files across all nodes",
			Action: largestWithNodes,
			Flags: []cli.Flag{
				cli.StringSliceFlag{
					Name:  "node, n",
					Usage: "A node to run the cmd on. Use multiple times",
				},
				cli.StringFlag{
					Name:  "nodes",
					Usage: "A comma separated list of nodes for the cmd",
				},
				cli.BoolFlag{
					Name:  "sudo",
					Usage: "Set to true for sudo",
				},
			},
		},
	}

	app.Run(os.Args)
}

func sshcmd(c *cli.Context) {
	command := c.String("cmd")
	debug := true

	command = fmt.Sprintf("/bin/bash <<CMD\nexport PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin:/root/bin\n%s\nCMD", command)

	if debug {
		color.Printf("@{b}%s\n", command)
	}

	var conns []string
	if c.IsSet("nodes") {
		conns = filterHosts(strings.Split(c.String("nodes"), ","))
	} else if c.IsSet("node") {
		conns = filterHosts(c.StringSlice("node"))
	} else {
		fmt.Errorf("Node or Nodes must be specified")
	}

	results := runSsh(conns, command, debug)
	fmt.Println(results)
}

func largestWithNodes(c *cli.Context) {
	debug := true
	command := "find / ! -readable -prune -type f -printf '%s %p\n' 2> /dev/null | sort -nr | head -n 15"

	if c.Bool("sudo") == true {
		command = fmt.Sprintf("/usr/bin/sudo -n bash <<CMD\nexport PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin:/root/bin\n%s\nCMD", command)
	} else {
		command = fmt.Sprintf("/bin/bash <<CMD\nexport PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin:/root/bin\n%s\nCMD", command)
	}

	if debug {
		color.Printf("@{b}%s\n", command)
	}

	var conns []string
	if c.IsSet("nodes") {
		conns = filterHosts(strings.Split(c.String("nodes"), ","))
	} else if c.IsSet("node") {
		conns = filterHosts(c.StringSlice("node"))
	} else {
		fmt.Errorf("Node or Nodes must be specified")
	}

	pathToSizeAndNodes := make(map[string]sizeByNodes)

	results := runSsh(conns, command, debug)
	for _, r := range results {
		lines := strings.Split(r.Res, "\n")
		for _, line := range lines {
			items := strings.Fields(line)
			fmt.Println(items[0])
			fmt.Println(items[1])

			path := items[1]

			if _, ok := pathToSizeAndNodes[path]; ok == false {
				size, err := strconv.ParseUint(items[0], 0, 0)
				if err != nil {
					panic(err)
				}
				pathToSizeAndNodes[path] = sizeByNodes{Size: size, Nodes: []string{r.Host}}
			} else {
				nodes := append(pathToSizeAndNodes[path].Nodes, r.Host)
				pathToSizeAndNodes[path] = sizeByNodes{Size: pathToSizeAndNodes[path].Size, Nodes: nodes}
			}
		}
	}

	for _, res := range sortedKeys(pathToSizeAndNodes) {
		fmt.Printf("%s\t%s\t%v\n", humanize.Bytes(pathToSizeAndNodes[res].Size), res, pathToSizeAndNodes[res].Nodes)
	}
}

type sizeByNodes struct {
	Nodes []string
	Size  uint64
}
type sortedMap struct {
	m map[string]sizeByNodes
	s []string
}

func (sm *sortedMap) Len() int {
	return len(sm.m)
}

func (sm *sortedMap) Less(i, j int) bool {
	return sm.m[sm.s[i]].Size > sm.m[sm.s[j]].Size
}

func (sm *sortedMap) Swap(i, j int) {
	sm.s[i], sm.s[j] = sm.s[j], sm.s[i]
}

func sortedKeys(m map[string]sizeByNodes) []string {
	sm := new(sortedMap)
	sm.m = m
	sm.s = make([]string, len(m))
	i := 0
	for key, _ := range m {
		sm.s[i] = key
		i++
	}
	sort.Sort(sm)
	return sm.s
}

func runSsh(conns []string, command string, debug bool) []Result {
	var wg sync.WaitGroup
	queue := make(chan Result)
	count := new(int)
	// Change if you need
	workers := 24
	var results []Result

	for _, conn := range conns {
		wg.Add(1)
		*count++
		if debug {
			color.Printf("@{y}%s\t\tcounter %3d\n", conn, *count)
		}
		for *count >= workers {
			time.Sleep(10 * time.Millisecond)
		}
		go func(h string) {
			defer wg.Done()
			var r Result

			r.Host = h
			client, err := ssh.Dial("tcp", h, config)
			if err != nil {
				color.Printf("@{!r}%s: Failed to connect: %s\n", h, err.Error())
				*count--
				if debug {
					color.Printf("@{y}%s\t\tcounter %3d\n", conn, *count)
				}
				return
			}

			session, err := client.NewSession()
			if err != nil {
				color.Printf("@{!r}%s: Failed to create session: %s\n", h, err.Error())
				*count--
				if debug {
					color.Printf("@{y}%s\t\tcounter %3d\n", conn, *count)
				}
				return
			}
			defer session.Close()

			// Set up terminal modes
			modes := ssh.TerminalModes{
				ssh.ECHO:          0,     // disable echoing
				ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
				ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
			}

			var b bytes.Buffer
			var e bytes.Buffer
			session.Stdout = &b
			session.Stderr = &e
			if err := session.RequestPty("xterm", 80, 40, modes); err != nil {
				log.Fatalf("request for pseudo terminal failed: %s", err)
			}

			if err := session.Start(command); err != nil {
				color.Printf("@{!r}%s: Failed to run: %s\n", h, err.Error())
				color.Printf("@{!r}%s\n", strip(e.String()))
				*count--
				if debug {
					color.Printf("@{y}%s\t\tcounter %3d\n", conn, *count)
				}
				return
			}

			if err := session.Wait(); err != nil {
				log.Fatalf("Command failed: %s", err)
			}

			r.Res = strip(b.String())

			color.Printf("@{!g}%s\n", r.Host)
			fmt.Println(r.Res)

			*count--
			if debug {
				color.Printf("@{y}%s\t\tcounter %3d\n", conn, *count)
			}

			runtime.Gosched()
			queue <- r
		}(conn)
	}
	go func() {
		defer wg.Done()
		for r := range queue {
			results = append(results, r)
		}
	}()
	wg.Wait()

	return results
}
