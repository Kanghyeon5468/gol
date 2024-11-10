package main

import (
	"flag"
	"log"
	"net"
	"net/rpc"
	"uk.ac.bris.cs/gameoflife/stubs"
)

type Node struct {
	LeftNeighbor  *Node
	RightNeighbor *Node
	WholeWorld    [][]uint8
	Start         int
	End           int
	Width         int
}

var quitting = make(chan bool, 1)

// GetSegment은 특정 구간에 대한 계산을 요청하고 결과를 반환하는 RPC 메서드입니다.
func (n *Node) GetSegment(req stubs.WorkerRequest, res *stubs.WorkerResponse) error {
	res.Segment = calculateNextWorld(req.WholeWorld, req.Start, req.End, req.Size)
	return nil
}

// Quit은 종료 신호를 받으면 채널을 통해 종료하도록 합니다.
func (n *Node) Quit(_ stubs.WorkerRequest, _ *stubs.WorkerResponse) error {
	quitting <- true
	return nil
}

// calculateNextWorld는 특정 구간의 게임의 다음 상태를 계산하는 함수입니다.
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

// calculateNeighbor는 (x, y) 위치의 이웃 셀 수를 계산하는 함수입니다.
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

// exchangeHalo는 인접한 워커와 halo region을 교환하는 메서드입니다.
func (n *Node) exchangeHalo() {
	// 왼쪽 이웃 워커로부터 상단 경계 받아오기
	if n.LeftNeighbor != nil {
		n.LeftNeighbor.GetSegment(stubs.WorkerRequest{
			WholeWorld: n.WholeWorld,
			Start:      n.Start - 1,
			End:        n.Start,
			Size:       n.Width,
		}, &stubs.WorkerResponse{})
	}

	// 오른쪽 이웃 워커로부터 하단 경계 받아오기
	if n.RightNeighbor != nil {
		n.RightNeighbor.GetSegment(stubs.WorkerRequest{
			WholeWorld: n.WholeWorld,
			Start:      n.End,
			End:        n.End + 1,
			Size:       n.Width,
		}, &stubs.WorkerResponse{})
	}
}

// 워커가 게임의 다음 상태를 계산하고, halo region을 교환합니다.
func (n *Node) process() {
	// Halo exchange 후, 계산을 시작합니다.
	n.exchangeHalo()

	// 현재 워커의 그리드에서 게임의 규칙을 적용하여 계산 진행
	n.WholeWorld = calculateNextWorld(n.WholeWorld, n.Start, n.End, n.Width)
}

// updateWorkers는 모든 워커가 게임 상태를 갱신하는 방식입니다.
func updateWorkers(workers []*Node) {
	for _, worker := range workers {
		go worker.process()
	}
}

func main() {
	serverPort := flag.String("port", "8040", "Port to Listen")
	flag.Parse()

	rpc.Register(&Node{})
	listener, err := net.Listen("tcp", "0.0.0.0:"+*serverPort)
	if err != nil {
		log.Fatal("Listener error:", err)
	}
	log.Println("Server listening on port", *serverPort)
	defer listener.Close()
	go rpc.Accept(listener)

	// 워커들 설정
	worker1 := &Node{Start: 0, End: 4, Width: 12}
	worker2 := &Node{Start: 4, End: 8, Width: 12, LeftNeighbor: worker1}
	worker3 := &Node{Start: 8, End: 12, Width: 12, LeftNeighbor: worker2}
	worker1.RightNeighbor = worker2
	worker2.RightNeighbor = worker3

	workers := []*Node{worker1, worker2, worker3}

	// 워커들을 업데이트
	updateWorkers(workers)

	// 종료 대기
	_ = <-quitting
}
