package main

import (
	"context"
	"flag"
	v3 "github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/clientv3/concurrency"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func init_etcd_client() *v3.Client {
	c, err := v3.New(v3.Config{
		Endpoints:   []string{"http://127.0.0.1:2379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatal("can't connect to etcd:", err)
	}
	return c
}

func finish_etcd_client(c *v3.Client) {
	c.Close()
}

func perform_unlock(c *v3.Client, lock_name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	s, err := concurrency.NewSession(c)
	if err != nil {
		log.Fatal("couldn't init session:", err)
	}
	defer s.Close()
	m := concurrency.NewMutex(s, "/distlock/"+lock_name)
	if err := m.Unlock(ctx); err != nil {
		log.Fatal("couldn't free lock:", err)
	}
	cancel()
	return
}

func perform_lock(c *v3.Client, lock_name string, reason string, timeout int) {
	var ctx context.Context
	var cancel context.CancelFunc
	if timeout <= 0 {
		ctx, cancel = context.WithCancel(context.Background())
	} else {
		ctx, cancel = context.WithTimeout(context.Background(),
			time.Duration(timeout)*time.Second)
	}
	s, err := concurrency.NewSession(c)
	if err != nil {
		log.Fatal("couldn't init session:", err)
	}
	defer s.Close()
	m := concurrency.NewMutex(s, "/distlock/"+lock_name)
	if err := m.Lock(ctx); err != nil {
		log.Fatal("couldn't acquire lock:", err)
	}
	cancel()
	return
}

func perform_command() int {
	args := flag.Args()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Fatal("distlock: cmd execution failed: ", err)
	}
	if err := cmd.Wait(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			/* exiterr is now type asserted to be type ExitError */
			status, ok := exiterr.Sys().(syscall.WaitStatus)
			if ok {
				/* status is type asserted to be a WaitStatus */
				return status.ExitStatus()
			} else {
				log.Fatal("distlock: unexpected WaitStatus")
			}
		} else {
			log.Fatal("distlock: unexpected ExitError")
		}
	}
	return 0
}

func main() {
	log.SetFlags(0)

	lock_name := flag.String("lock-name", "",
		"Name of the lock to operate on")
	op_lock := flag.Bool("lock", false, "Acquire lock and exit")
	op_unlock := flag.Bool("unlock", false, "Release lock and exit")
	reason := flag.String("reason", "",
		"Reason why we perform this operation")
	no_wait := flag.Bool("nowait", false, "Fail if the lock is busy")
	timeout := flag.Int("timeout", -1,
		"Max. no. of secs to wait for the lock")

	flag.Parse()

	if *lock_name == "" {
		log.Fatal("'lock-name' is a required option.")
	}
	if *op_lock && *op_unlock {
		log.Fatal("Can't give both 'lock' and 'unlock' options.")
	}
	if (*op_lock || *op_unlock) && flag.NArg() > 0 {
		log.Fatal("Program args given, but would not execute.")
	}
	if *no_wait {
		if *timeout > 0 {
			log.Fatal("Conflicting options -nowait and -timeout.")
		} else {
			*timeout = 0
		}
	}
	if !*op_lock && !*op_unlock && flag.NArg() == 0 {
		log.Fatal("Missing command to protect with lock")
	}

	c := init_etcd_client()
	defer finish_etcd_client(c)

	if *op_unlock {
		perform_unlock(c, *lock_name)
		os.Exit(0)
	} else {
		perform_lock(c, *lock_name, *reason, *timeout)
		if *op_lock {
			/* we're done */
			os.Exit(0)
		} else {
			rc := perform_command()
			perform_unlock(c, *lock_name)
			os.Exit(rc)
		}
	}
}
