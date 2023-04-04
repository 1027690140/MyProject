package dag

import (
	"fmt"
	queue "rpc_service/util"
	"sync"
)

/*
任务编排调度
*/

// 图结构
type DAG struct {
	Vertexs []*Vertex
}

// 顶点
type Vertex struct {
	Key      string
	Value    interface{}
	Parents  []*Vertex
	Children []*Vertex
	taskfunc func(...interface{}) (interface{}, error)
}

// 添加顶点
func (dag *DAG) AddVertex(v *Vertex) {
	dag.Vertexs = append(dag.Vertexs, v)
}

// 添加边
func (dag *DAG) AddEdge(from, to *Vertex) {
	from.Children = append(from.Children, to)
	to.Parents = append(to.Parents, from)
}

// 生成图，返回dag和其根顶点
func NewDAG(root *Vertex) (*DAG, *Vertex) {
	var dag = &DAG{}
	dag.Vertexs = append(dag.Vertexs, root)
	return dag, root
}

// 广度遍历 返回双层结构
func BFS(root *Vertex) [][]*Vertex {

	q := queue.NewQueue()
	q.Add(root)
	visited := make(map[string]*Vertex)
	all := make([][]*Vertex, 0)

	for q.Length() > 0 {
		qSize := q.Length()
		tmp := make([]*Vertex, 0)
		for i := 0; i < qSize; i++ {
			//pop vertex
			currVert := q.Remove().(*Vertex)
			if _, ok := visited[currVert.Key]; ok {
				continue
			}
			visited[currVert.Key] = currVert
			tmp = append(tmp, currVert)

			//fmt.Println(level, currVert.Key, currVert.Value)

			for _, val := range currVert.Children {
				if _, ok := visited[val.Key]; !ok {
					q.Add(val) //add child
				}
			}
		}
		all = append([][]*Vertex{tmp}, all...)
	}
	return all
}

// 分层执行，上层任务依赖下层，但每一层的任务相互独立可以并发执行。
func doTasks(vertexs []*Vertex) {
	var wg sync.WaitGroup
	for _, v := range vertexs {
		wg.Add(1)
		go func(v *Vertex) error {
			defer wg.Done()
			_, err := v.taskfunc(v.Value)
			if err != nil {
				return fmt.Errorf(" %v, func is fail \n", v.Key)
			}
			fmt.Printf("do %v, result is %v \n", v.Key, v.Value)

			return nil
		}(v) //notice
	}
	wg.Wait()
}
