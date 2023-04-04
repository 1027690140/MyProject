package dag

import (
	"fmt"
	"testing"
)

func TestDag(t *testing.T, v *Vertex) {
	_, root := NewDAG(v)
	all := BFS(root)

	for _, layer := range all {
		fmt.Println("------------------")
		doTasks(layer)
	}
}

func main() {
	var dag = &DAG{}
	va := &Vertex{Key: "a", Value: "1"}
	vb := &Vertex{Key: "b", Value: "2"}
	vc := &Vertex{Key: "c", Value: "3"}
	vd := &Vertex{Key: "d", Value: "4"}
	ve := &Vertex{Key: "e", Value: "5"}
	vf := &Vertex{Key: "f", Value: "6"}
	vg := &Vertex{Key: "g", Value: "7"}
	vh := &Vertex{Key: "h", Value: "8"}
	vi := &Vertex{Key: "i", Value: "9"}
	vx := &Vertex{Key: "x", Value: "10"}
	vy := &Vertex{Key: "y", Value: "11"}
	dag.AddEdge(va, vb)
	dag.AddEdge(va, vc)
	dag.AddEdge(va, vd)
	dag.AddEdge(vb, ve)
	dag.AddEdge(vb, vh)
	dag.AddEdge(vb, vf)
	dag.AddEdge(vc, vf)
	dag.AddEdge(vc, vg)
	dag.AddEdge(vd, vg)
	dag.AddEdge(vh, vi)
	dag.AddEdge(ve, vi)
	dag.AddEdge(vf, vi)
	dag.AddEdge(vg, vi)
	dag.AddEdge(vx, vd)
	dag.AddEdge(vy, vi)
	TestDag(&testing.T{}, va)
}
