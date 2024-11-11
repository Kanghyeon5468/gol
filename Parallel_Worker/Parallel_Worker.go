package main

import (
	"flag"
	"log"
	"net"
	"net/rpc"
	"sync"
	"uk.ac.bris.cs/gameoflife/stubs"
)

var wg sync.WaitGroup

type ParallelNode struct{}

type pixel struct {
	X     int
	Y     int
	Value uint8
}

var quitting = make(chan bool, 1)

func (n *ParallelNode) GetSegment(req stubs.ParallelWorkerRequest, res *stubs.ParallelWorkerResponse) error {
	res.Segment = calculateNewAliveParallel(req.Threads, req.WholeWorld, req.Start, req.End, req.Size)
	return nil
}

func (n *ParallelNode) Quit(_ stubs.WorkerRequest, _ *stubs.WorkerResponse) error {
	quitting <- true
	return nil
}

func calculateNewAliveParallel(workerNum int, world [][]uint8, start, end, worldSize int) [][]uint8 {
	//numRows := p.ImageHeight / workerNum

	splitSegments := make([]chan pixel, workerNum)
	for i := range splitSegments {
		splitSegments[i] = make(chan pixel, worldSize*worldSize)
	}
	// start workers to make the world
	//startWorkers(workerNum, numRows, p, dataChan, neighbourChan, c)
	setupWorkers(worldSize, start, end, workerNum, splitSegments, world, start == 0, end == worldSize)

	//var cells []util.Cell

	wg.Wait()
	for i := 0; i < workerNum; i++ {
		length := len(splitSegments[i])
		for j := 0; j < length; j++ {
			item := <-splitSegments[i]
			world[item.X][item.Y] = item.Value
			//cells = append(cells, util.Cell{X: item.X, Y: item.Y})
		}
	}

	return world[start:end]
}

func setupWorkers(worldHeight, sectionStart, sectionEnd, workerNum int, splitSegments []chan pixel, world [][]uint8, starting, ending bool) {
	sectionHeight := sectionEnd - sectionStart
	numRows := sectionHeight / workerNum

	upperBoundChan := make(chan []uint8, workerNum)
	lowerBoundChan := make(chan []uint8, workerNum)

	if starting && ending {
		wg.Add(1)
		go runWorker(worldHeight, 0, sectionHeight, splitSegments[0], world, true, true, upperBoundChan, lowerBoundChan)
		return
	}

	if workerNum == 1 {
		wg.Add(1)
		go runWorker(worldHeight, sectionStart, sectionStart+numRows, splitSegments[0], world, starting, ending, upperBoundChan, lowerBoundChan)
		return
	}

	wg.Add(1)
	go runWorker(worldHeight, sectionStart, sectionStart+numRows, splitSegments[0], world, starting, false, upperBoundChan, lowerBoundChan)

	for i := 1; i < workerNum-1; i++ {
		wg.Add(1)
		go runWorker(worldHeight, sectionStart+i*numRows, sectionStart+(i+1)*numRows, splitSegments[i], world, false, false, upperBoundChan, lowerBoundChan)
	}

	if workerNum > 1 {
		wg.Add(1)
		go runWorker(worldHeight, sectionStart+(workerNum-1)*numRows, sectionStart+sectionHeight, splitSegments[workerNum-1], world, false, ending, upperBoundChan, lowerBoundChan)
	}
}

func runWorker(worldHeight, start, end int, splitSegment chan pixel, world [][]uint8, starting, ending bool, upperBoundChan, lowerBoundChan chan []uint8) {
	defer wg.Done()

	if !starting {
		// get upperbound from prev worker
		upperBound := <-upperBoundChan
		world[start-1] = upperBound
	}

	if !ending {
		// get lowerbound from next worker
		lowerBound := <-lowerBoundChan
		world[end] = lowerBound
	}

	neighboursWorld := calculateNeighbours(start, end, worldHeight, world, starting, ending)
	calculateNextWorld(start, end, worldHeight, splitSegment, world, neighboursWorld)

	if !starting {
		//send upperbound to prev worker
		upperBoundChan <- world[start]
	}

	if !ending {
		//send lowerbound to next worker
		lowerBoundChan <- world[end-1]
	}
}

//func setupWorkers(worldHeight, sectionStart, sectionEnd, workerNum int, splitSegments []chan pixel, world [][]uint8, starting, ending bool) {
//	sectionHeight := sectionEnd - sectionStart
//	numRows := sectionHeight / workerNum
//
//
//	// you are one worker calculating the entire world
//	if starting && ending {
//		wg.Add(1)
//		go runWorker(worldHeight, 0, sectionHeight, splitSegments[0], world, true, true)
//		return
//	}
//
//	if workerNum == 1 {
//		wg.Add(1)
//		go runWorker(worldHeight, sectionStart, sectionStart+numRows, splitSegments[0], world, starting, ending)
//		return
//	}
//
//	wg.Add(1)
//	go runWorker(worldHeight, sectionStart, sectionStart+numRows, splitSegments[0], world, starting, false)
//
//	i := 1
//	for i < workerNum-1 {
//		wg.Add(1)
//		go runWorker(worldHeight, sectionStart+i*numRows, sectionStart+(i+1)*numRows, splitSegments[i], world, false, false)
//		i++
//	}
//
//	if workerNum > 1 {
//		// final worker does the remaining rows
//		wg.Add(1)
//		go runWorker(worldHeight, sectionStart+i*numRows, sectionStart+sectionHeight, splitSegments[i], world, false, ending)
//	}
//}
//
//func runWorker(worldHeight, start, end int, splitSegment chan pixel, world [][]uint8, starting, ending bool) {
//	defer wg.Done()
//	neighboursWorld := calculateNeighbours(start, end, worldHeight, world, starting, ending)
//	calculateNextWorld(start, end, worldHeight, splitSegment, world, neighboursWorld)
//}

//func runWorkerStart(size, worldHeight, start, end int, splitSegment chan pixel, world [][]uint8) {
//	defer wg.Done()
//	neighboursWorld := calculateNeighbours(start, end, worldHeight, world)
//	calculateNextWorld(start, end, size, splitSegment, world, neighboursWorld)
//}
//
//func runWorkerFull(size, worldHeight, start, end int, splitSegment chan pixel, world [][]uint8) {
//	defer wg.Done()
//	neighboursWorld := calculateNeighboursFull(start, end, worldHeight, world)
//	calculateNextWorld(start, end, size, splitSegment, world, neighboursWorld)
//}
//
//func runWorkerMiddle(size, worldHeight, start, end int, splitSegment chan pixel, world [][]uint8) {
//	defer wg.Done()
//	neighboursWorld := calculateNeighbours(start, end, worldHeight, world)
//	calculateNextWorld(start, end, size, splitSegment, world, neighboursWorld)
//}
//
//func runWorkerEnd(size, worldHeight, start, end int, splitSegment chan pixel, world [][]uint8) {
//	defer wg.Done()
//	neighboursWorld := calculateNeighbours(start, end, worldHeight, world)
//	calculateNextWorld(start, end, size, splitSegment, world, neighboursWorld)
//}

func calculateNextWorld(start, end, width int, c chan pixel, world [][]uint8, neighboursWorld [][]int) {
	for y := start; y < end; y++ {
		for x := 0; x < width; x++ {
			neighbors := neighboursWorld[y][x]
			if (neighbors < 2 || neighbors > 3) && world[y][x] == 255 {
				c <- pixel{y, x, 0}
			} else if neighbors == 3 && world[y][x] == 0 {
				c <- pixel{y, x, 255}
			}
		}
	}
}

func calculateNeighbours(start, end, width int, world [][]uint8, starting, ending bool) [][]int {
	neighbours := make([][]int, width)
	for i := range neighbours {
		neighbours[i] = make([]int, width)
	}

	if !starting && !ending {
		start--
		end++
	}

	for y := start; y < end; y++ {
		for x := 0; x < width; x++ {
			neighbours[y][x] = calculateNeighbor(x, y, world, width)
		}
	}

	return neighbours
}

func calculateNeighbor(x, y int, world [][]uint8, width int) int {
	aliveNeighbor := 0
	for i := -1; i <= 1; i++ {
		for j := -1; j <= 1; j++ {
			if i == 0 && j == 0 {
				continue
			}

			nx, ny := (x+j+width)%width, (y+i+width)%width
			if world[ny][nx] == 255 {
				aliveNeighbor++
			}
		}
	}
	return aliveNeighbor
}

//func calculateNeighboursFull(start, end, width int, world [][]uint8) [][]int {
//	neighbours := make([][]int, width)
//	for i := range neighbours {
//		neighbours[i] = make([]int, width)
//	}
//
//	for y := start; y < end; y++ {
//		for x := 0; x < width; x++ {
//			// if a cell is 255
//			if world[y][x] == 255 {
//				// add 1 to all neighbours
//				// i and j are the offset
//				for i := -1; i <= 1; i++ {
//					for j := -1; j <= 1; j++ {
//
//						// if you are not offset, do not add one. This is yourself
//						if !(i == 0 && j == 0) {
//							ny, nx := y+i, x+j
//							if nx < 0 {
//								nx = width - 1
//							} else if nx == width {
//								nx = 0
//							} else {
//								nx = nx % width
//							}
//
//							if ny < 0 {
//								ny = width - 1
//							} else if ny == width {
//								ny = 0
//							} else {
//								ny = ny % width
//							}
//
//							neighbours[ny][nx]++
//						}
//
//					}
//				}
//			}
//		}
//	}
//
//	return neighbours
//}

//func calculateNeighbours(start, end, width int, world [][]uint8, starting, ending bool) [][]int {
//	neighbours := make([][]int, width)
//	for i := range neighbours {
//		neighbours[i] = make([]int, width)
//	}
//
//	if !(starting && ending) {
//		if starting {
//			for x := 0; x < width; x++ {
//				if world[width-1][x] == 255 {
//					for i := -1; i <= 1; i++ {
//						//for image wrap around
//						xCoord := x + i
//						if xCoord < 0 {
//							xCoord = width - 1
//						} else if xCoord >= width {
//							xCoord = 0
//						}
//						neighbours[0][xCoord]++
//					}
//				}
//			}
//		} else {
//			start--
//		}
//
//		if ending {
//			for x := 0; x < width; x++ {
//				if world[0][x] == 255 {
//					for i := -1; i <= 1; i++ {
//						//for image wrap around
//						xCoord := x + i
//						if xCoord < 0 {
//							xCoord = width - 1
//						} else if xCoord >= width {
//							xCoord = 0
//						}
//						neighbours[width-1][xCoord]++
//					}
//				}
//			}
//		} else {
//			end++
//		}
//	}
//
//	for y := start; y < end; y++ {
//		for x := 0; x < width; x++ {
//			// if a cell is 255
//			if world[y][x] == 255 {
//				// add 1 to all neighbours
//				// i and j are the offset
//				for i := -1; i <= 1; i++ {
//					for j := -1; j <= 1; j++ {
//
//						// if you are not offset, do not add one. This is yourself
//						if !(i == 0 && j == 0) {
//							ny, nx := y+i, x+j
//							if nx < 0 {
//								nx = width - 1
//							} else if nx == width {
//								nx = 0
//							} else {
//								nx = nx % width
//							}
//
//							if ny < 0 {
//								ny = width - 1
//							} else if ny == width {
//								ny = 0
//							} else {
//								ny = ny % width
//							}
//
//							neighbours[ny][nx]++
//						}
//
//					}
//				}
//			}
//		}
//	}
//
//	return neighbours
//}

func main() {
	serverPort := flag.String("port", "8040", "Port to Listen")
	flag.Parse()

	rpc.Register(&ParallelNode{})
	listener, err := net.Listen("tcp", "0.0.0.0:"+*serverPort)
	if err != nil {
		log.Fatal("Listener error:", err)
	}
	log.Println("Server listening on port", *serverPort)
	defer listener.Close()
	go rpc.Accept(listener)

	_ = <-quitting
}
