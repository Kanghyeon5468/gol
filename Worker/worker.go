package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/rpc"
	"sync"
	"uk.ac.bris.cs/gameoflife/stubs"
)

var nextWorker *rpc.Client

var lowerBoundGive, lowerBoundGet, upperBoundGet, upperBoundGive []uint8

var calculatingLowerBound, receivingLowerBound sync.WaitGroup

type pixel struct {
	X     int
	Y     int
	Value uint8
}

type Node struct {
}

var quitting = make(chan bool, 1)

//func (n *Node) SendUpperBound(req stubs.WorkerRequest, res *stubs.WorkerResponse) error {
//	if req.Worker != nil {
//		err := req.Worker.Call("Node.ReceiveUpperBound", req, res)
//		if err != nil {
//			log.Println("Error sending upper bound:", err)
//			return err
//		}
//	}
//	return nil
//}
//
//func (n *Node) SendLowerBound(req stubs.WorkerRequest, res *stubs.WorkerResponse) error {
//	if req.Worker != nil {
//		err := req.Worker.Call("Node.ReceiveLowerBound", req, res)
//		if err != nil {
//			log.Println("Error sending lower bound:", err)
//			return err
//		}
//	}
//	return nil
//}
//
//func (n *Node) ReceiveUpperBound(req stubs.WorkerRequest) error {
//	req.UpperBound = req.WholeWorld[req.Size-1 : req.Size]
//	log.Println("Received upper boundary:", req.UpperBound)
//	return nil
//}
//	func (n *Node) ReceiveLowerBound(req stubs.WorkerRequest) error {
//		req.LowerBound = req.WholeWorld[0:1] // Store the lower row
//		log.Println("Received lower boundary:", req.LowerBound)
//		return nil
//	}

//func upperBound(world [][]uint8, end, start, size int) [][]uint8 {
//	var upperBound [][]uint8
//	if start == 0 {
//		upperBound = world[size-1 : size]
//	} else {
//		upperBound = world[start-1 : start]
//	}
//	world = append(upperBound, world...)
//	return world
//}
//
//func lowerBound(world [][]uint8, end, start, size int) [][]uint8 {
//	var lowerBound [][]uint8
//	if end == size-1 {
//		lowerBound = world[0:1]
//	} else {
//		lowerBound = world[end+1 : end+2]
//	}
//	world = append(lowerBound, world...)
//	return world
//}

func (n *Node) GetSegment(req stubs.WorkerRequest, res *stubs.WorkerResponse) error {
	res.Segment = calculateNextWorld(req.WholeWorld, req.Start, req.End, req.Size)
	return nil
}

func (n *Node) Quit(_ stubs.WorkerRequest, _ *stubs.WorkerResponse) error {
	quitting <- true
	return nil
}

func calculateNextWorld(world [][]uint8, start, end, width int) [][]uint8 {
	newWorld := make([][]uint8, end-start)
	for i := 0; i < end-start; i++ {
		newWorld[i] = make([]uint8, width)
	}
	for y := start; y < end; y++ {
		for x := 0; x < width; x++ {
			neighbors := calculateNeighbor(x, y, world, width)
			if neighbors < 2 || neighbors > 3 {
				newWorld[y-start][x] = 0
			} else if neighbors == 3 {
				newWorld[y-start][x] = 255
			} else if neighbors == 2 && world[y][x] == 255 {
				newWorld[y-start][x] = 255
			} else {
				newWorld[y-start][x] = 0
			}
		}
	}
	return newWorld
}

func calculateNeighbor(x, y int, world [][]uint8, width int) int {
	aliveNeighbor := 0
	for i := -1; i <= 1; i++ {
		for j := -1; j <= 1; j++ {
			ny, nx := y+i, x+j
			if nx < 0 {
				nx = width - 1
			} else if nx == width {
				nx = 0
			} else {
				nx = nx % width
			}

			if ny < 0 {
				ny = width - 1
			} else if ny == width {
				ny = 0
			} else {
				ny = ny % width
			}

			if world[ny][nx] == 255 {
				if !(i == 0 && j == 0) {
					aliveNeighbor++
				}
			}
		}
	}
	return aliveNeighbor
}

func (n *Node) ConnectToNextWorker(req stubs.AddressReq, _ *stubs.EmptyRes) error {
	worker, err := rpc.Dial("tcp", req.Address)
	if err != nil {
		return err
	}
	nextWorker = worker
	return nil
}

func (n *Node) AcceptPartialRows(req stubs.HaloRowsReq, res stubs.HaloRowsRes) error {
	lowerBoundGet = req.Row
	receivingLowerBound.Done()
	calculatingLowerBound.Wait()
	res.Row = lowerBoundGive
	return nil
}

func giveRowsWorker(partialRow []uint8) []uint8 {
	res := new(stubs.HaloRowsRes)
	req := stubs.HaloRowsReq{
		Row: partialRow,
	}
	if err := nextWorker.Call(stubs.PartialRows, req, &res); err != nil {
		fmt.Println(err)
	}
	return res.Row
}

func main() {
	serverPort := flag.String("port", "8040", "Port to Listen")
	flag.Parse()

	// Initialize Node and neighbors map
	node := &Node{}

	rpc.Register(node)
	listener, err := net.Listen("tcp", "0.0.0.0:"+*serverPort)
	if err != nil {
		log.Fatal("Listener error:", err)
	}
	log.Println("Server listening on port", *serverPort)
	defer listener.Close()
	go rpc.Accept(listener)

	_ = <-quitting
}
