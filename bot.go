package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	mpv "github.com/majiru/mpvipc"
	tb "gopkg.in/tucnak/telebot.v2"
)

var UserFlag = flag.String("users", "", "Whitelist of users that can control the bot")
var UserWhitelist map[string]struct{} = nil

var GroupFlag = flag.Int64("group", 0, "Group ID that can control the bot")

var LibFlag = flag.String("lib", "", "Video library")

func parseUsers() error {
	if *UserFlag == "" {
		return errors.New("No users defined")
	}
	s := strings.Split(*UserFlag, ",")
	UserWhitelist = make(map[string]struct{})
	for _, u := range s {
		UserWhitelist[u] = struct{}{}
	}
	return nil
}

func permitted(username string, groupID int64) bool {
	var (
		userOK  = true
		groupOK = true
	)
	if UserWhitelist != nil {
		_, userOK = UserWhitelist[username]
	}
	if *GroupFlag != 0 {
		groupOK = (groupID == *GroupFlag)
	}
	return userOK && groupOK
}

type Index struct {
	*sync.RWMutex
	m []string
}

func (index *Index) find(i int) (string, error) {
	index.RLock()
	if i > len(index.m) {
		return "", errors.New("index OOB")
	}
	s := index.m[i]
	index.RUnlock()
	return s, nil
}

func (index *Index) build(path string, query string) error {
	index.Lock()
	defer index.Unlock()
	index.m = []string{}
	dirs, err := ioutil.ReadDir(path)
	if err != nil {
		return err
	}
	for _, s := range dirs {
		if query == "" {
			index.m = append(index.m, filepath.Join(path, s.Name()))
		} else if strings.Contains(strings.ToLower(s.Name()), query) {
			index.m = append(index.m, filepath.Join(path, s.Name()))
		}
	}
	return nil
}

func (index *Index) String() string {
	var b strings.Builder
	index.RLock()
	for i, s := range index.m {
		b.WriteString(fmt.Sprintf("%d\t%s\n", i, s))
	}
	index.RUnlock()
	return b.String()
}

var Cache = &Index{&sync.RWMutex{}, []string{}}

func main() {
	flag.Parse()
	parseUsers()

	b, err := tb.NewBot(tb.Settings{
		Token:  os.Getenv("BOTTOKEN"),
		Poller: &tb.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatal(err)
	}

	conn := mpv.NewConnection("/tmp/mpv_sock")
	err = conn.Open()
	if err != nil {
		log.Fatal(err)
	}

	b.Handle("/play", func(m *tb.Message) {
		if !permitted(m.Sender.Username, m.Chat.ID) {
			b.Reply(m, fmt.Sprintf("You not permitted to play videos\nGroup ID: %d\nUsername: %s", m.Chat.ID, m.Sender.Username))
			return
		}
		conn.Call("loadfile", m.Payload)
	})

	b.Handle("/playi", func(m *tb.Message) {
		if !permitted(m.Sender.Username, m.Chat.ID) {
			b.Reply(m, fmt.Sprintf("You not permitted to play videos\nGroup ID: %d\nUsername: %s", m.Chat.ID, m.Sender.Username))
			return
		}
		if m.Payload == "" {
			b.Reply(m, "Please give me an index")
			return
		}
		i, err := strconv.Atoi(m.Payload)
		if err != nil {
			b.Reply(m, fmt.Sprintf("strconv error: %v", err))
			return
		}
		s, err := Cache.find(i)
		if err != nil {
			b.Reply(m, fmt.Sprintf("index error: %v", err))
			return
		}
		conn.Call("loadfile", s)
	})

	b.Handle("/list", func(m *tb.Message) {
		if *LibFlag == "" {
			b.Reply(m, "No library specified, please define one on the command line during startup")
			return
		}
		Cache.build(*LibFlag, m.Payload)
		_, err = b.Reply(m, Cache.String())
		if err != nil {
			_, err2 := b.Reply(m, err.Error())
			if err2 != nil {
				log.Println(err)
			}
		}
	})

	b.Handle("/listi", func(m *tb.Message) {
		if *LibFlag == "" {
			b.Reply(m, "No library specified, please define one on the command line during startup")
			return
		}
		if m.Payload == "" {
			b.Reply(m, "Please give me an index")
			return
		}
		index := m.Payload
		query := ""
		p := strings.Split(m.Payload, " ")
		if len(p) == 2 {
			index = p[0]
			query = p[1]
		}
		i, err := strconv.Atoi(index)
		if err != nil {
			b.Reply(m, fmt.Sprintf("strconv error: %v", err))
			return
		}
		s, err := Cache.find(i)
		if err != nil {
			b.Reply(m, fmt.Sprintf("index error: %v", err))
			return
		}
		err = Cache.build(s, query)
		if err != nil {
			log.Fatal(err)
		}
		_, err = b.Reply(m, Cache.String())
		if err != nil {
			_, err2 := b.Reply(m, err.Error())
			if err2 != nil {
				log.Println(err)
			}
		}
	})

	b.Start()
}
