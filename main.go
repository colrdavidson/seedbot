package main

import (
	"fmt"
	"net"
	"os"
	"time"
	"sync"
	"strconv"
	"strings"
	"encoding/json"
	"io/ioutil"
)

const MSGLEN = 16384

type Server struct {
	conn net.Conn
	key []byte

	Name string `json:"name"`
	Ip string `json:"ip"`
	Port int `json:"port"`
	Password string `json:"password"`
}

type Rots struct {
	Seed   []string `json:"seed"`
	Normal []string `json:"normal"`
	Late   []string `json:"late"`
}

type Config struct {
	Rotations Rots `json:"rotations"`
	Servers []Server `json:"servers"`
}

func nowstr() (string) {
	return time.Now().UTC().Format(time.RFC822)
}

func send_to_server(srv *Server, msg []byte) (err error) {
	outbuf := make([]byte, len(msg))
	for i := 0; i < len(msg); i++ {
		outbuf[i] = msg[i] ^ srv.key[i % len(srv.key)]
	}

	_, err = srv.conn.Write(outbuf)
	return err
}

func recv_from_server(srv *Server, rbuf []byte) (size int, err error) {
	size, err = srv.conn.Read(rbuf)
	if err != nil {
		return 0, err
	}

	for i := 0; i < size; i++ {
		rbuf[i] = rbuf[i] ^ srv.key[i % len(srv.key)]
	}

	return size, err
}

func run_command(srv *Server, command string) (result string, err error) {
	err = send_to_server(srv, []byte(command))
	if err != nil {
		return "", err
	}

	rbuf := make([]byte, MSGLEN)
	ret_size, err := recv_from_server(srv, rbuf)
	if err != nil {
		return "", err
	}

	outbuffer := make([]byte, ret_size)
	copy(outbuffer, rbuf)

	for ret_size >= MSGLEN {
		size, err := recv_from_server(srv, rbuf)
		if err != nil {
			return "", err
		}

		buffer := make([]byte, size)
		copy(buffer, rbuf)

		outbuffer = append(outbuffer, buffer...)
		ret_size = size
	}

	return string(outbuffer), nil
}

func connect_to_server(srv *Server) (err error) {
	conn_str := fmt.Sprintf("%s:%d", srv.Ip, srv.Port)

	conn, err := net.Dial("tcp", conn_str)
	if err != nil {
		return err
	}

	temp_buf := make([]byte, MSGLEN)
	size, err := conn.Read(temp_buf)
	if err != nil {
		return err
	}

	srv.key = make([]byte, size)
	_ = copy(srv.key, temp_buf[:size])
	srv.conn = conn

	auth_msg := fmt.Sprintf("login %s", srv.Password)
	auth_status, err := run_command(srv, auth_msg)
	if err != nil {
		conn.Close()
		return err
	}

	if auth_status != "SUCCESS" {
		conn.Close()
		return nil
	}

	return nil
}

func get_rotation(srv *Server) (rotation []string, err error) {
	rotmaps_str, err := run_command(srv, "rotlist")
	if err != nil {
		return nil, err
	}

	rotmaps := strings.Split(rotmaps_str, "\n")
	rotmaps = rotmaps[:len(rotmaps)-1]

	return rotmaps, nil
}

func get_all_maps(srv *Server) (maps []string, err error) {
	maps_str, err := run_command(srv, "get mapsforrotation")
	if err != nil {
		return nil, err
	}

	maps = strings.Split(maps_str, "\t")
	maps = maps[1:len(maps)-1]

	return maps, nil
}

func rotate_maps(rot []string, off int) ([]string) {
	idx := len(rot) - off % len(rot)
	new_rot := append(rot[idx:], rot[:idx]...)
	return new_rot
}

func swap_rotation(config *Config, id int, mode string) (err error) {
	srv := &config.Servers[id]

	// Figure out what rotation we should be on
	var new_rot []string
	switch mode {
	case "seed":
		new_rot = config.Rotations.Seed
	case "late":
		new_rot = config.Rotations.Late
	case "normal":
		new_rot = config.Rotations.Normal
	default:
		new_rot = config.Rotations.Normal
	}

	cur_rot, err := get_rotation(srv)
	if err != nil {
		return err
	}

	// rotate the rotation by the id to make sure the servers don't all play the same map
	new_rot = rotate_maps(new_rot, id)

	need_refresh := false
	if len(cur_rot) != len(new_rot) {
		need_refresh = true
	} else {
		for i := 0; i < len(cur_rot); i++ {
			if cur_rot[i] != new_rot[i] {
				need_refresh = true
				break
			}
		}
	}

	if !need_refresh {
		return nil
	}

	fmt.Printf("[%d] %s | Shifting to {%s} mode\n", id, nowstr(), mode)

	for i := 0; i < len(cur_rot); i++ {
		cmd := fmt.Sprintf("rotdel %s", cur_rot[i])
		result, err := run_command(srv, cmd)
		if err != nil || result != "SUCCESS" {
			return err
		}
	}

	full_rot, err := get_all_maps(srv)
	if err != nil {
		return err
	}

	// Make a new rotation that's just the first thing in the new rotation
	first_map := new_rot[0]
	del_map := make(map[string]int)
	for i := 0; i < len(full_rot); i++ {
		if full_rot[i] != first_map {
			del_map[full_rot[i]] = 1
		}
	}

	for key, _ := range del_map {
		cmd := fmt.Sprintf("rotdel %s", key)
		result, err := run_command(srv, cmd)
		if err != nil || result != "SUCCESS" {
			return err
		}
	}

	// Add the rest of the new rotation back in
	for i := 1; i < len(new_rot); i++ {
		cmd := fmt.Sprintf("rotadd %s", new_rot[i])
		result, err := run_command(srv, cmd)
		if err != nil || result != "SUCCESS" {
			return err
		}
	}

	cur_rot, err = get_rotation(srv)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	if len(os.Args) != 2 {
		fmt.Printf("Seedbot requires a config file!\nEx: ./seedbot config.json\n")
		os.Exit(1)
	}

	filename := os.Args[1]
	config_file, err := os.Open(filename)
	if err != nil {
		panic(err)
	}

	config_bytes, _ := ioutil.ReadAll(config_file)
	config_file.Close()

	config := Config{}
	err = json.Unmarshal(config_bytes, &config)
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < len(config.Servers); i++ {
		wg.Add(1)

		go func(config *Config, id int) {
			defer wg.Done()

			srv := &config.Servers[id]

			err = connect_to_server(srv)
			if err != nil {
				panic(err)
			}

			result, err := run_command(srv, "get idletime")
			if err != nil {
				panic(err)
			}
			idle_time, err := strconv.Atoi(result)
			if err != nil {
				panic(err)
			}

			players, err := run_command(srv, "get players")
			if err != nil {
				panic(err)
			}
			idx := strings.Index(players, "\t")
			if idx == -1 {
				panic("Invalid player ret!\n")
			}

			count_str := players[:idx]
			player_count, err := strconv.Atoi(count_str)
			if err != nil {
				panic(err)
			}

			cur_map, err := run_command(srv, "get map")
			if err != nil {
				panic(err)
			}

			new_idle_time := idle_time

			if player_count >= 90 {
				new_idle_time = 10
			} else if player_count == 0 {
				new_idle_time = 9999
			}

			if idle_time != new_idle_time {
				fmt.Printf("[%d] %s | Setting new idle time: %d\n", id, nowstr(), new_idle_time)
				cmd := fmt.Sprintf("setkickidletime %d", new_idle_time)
				_, err = run_command(srv, cmd)

				if err != nil {
					panic(err)
				}
				idle_time = new_idle_time
			}

			rotation_mode := ""
			if idle_time == 9999 {
				rotation_mode = "seed"
			} else {
				cur_hour := time.Now().UTC().Hour()
				// If between 11 PM PST and 6 AM PST -- converted to 24hr UTC
				if cur_hour > 6 && cur_hour < 13 {
					rotation_mode = "late"
				} else {
					rotation_mode = "normal"
				}
			}

			err = swap_rotation(config, id, rotation_mode)
			if err != nil {
				panic(err)
			}

			fmt.Printf("[%d] %s | %s on %s @ %d -- {%s}\n", id, nowstr(), srv.Name, cur_map, player_count, rotation_mode)
			srv.conn.Close()
		}(&config, i)
		wg.Wait()
	}
}
