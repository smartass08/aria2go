package manifest

import "fmt"

// DAGViolation represents a single DAG validation failure.
type DAGViolation struct {
	Type    string
	Message string
}

func (v DAGViolation) Error() string {
	return fmt.Sprintf("%s: %s", v.Type, v.Message)
}

// DAGResult holds the complete DAG validation output.
type DAGResult struct {
	Violations   []DAGViolation
	NumTickets   int
	NumModules   int
	CriticalPath int
}

// ValidateDAG runs full DAG validation on a manifest and returns all
// violations. It checks:
//
//   - ticket depends_on DAG is acyclic (Tarjan SCC)
//   - module depends_on_modules DAG is acyclic (Tarjan SCC)
//   - depends_on references are all known ticket IDs
//   - target_files sharing requires a depends_on edge (transitive closure)
//
// Critical path is computed via topological ordering and DP.
func ValidateDAG(m *Manifest) *DAGResult {
	result := &DAGResult{
		NumTickets: len(m.Tickets),
		NumModules: len(m.Modules),
	}

	cycles := dagTarjanTickets(m)
	for _, cycle := range cycles {
		result.Violations = append(result.Violations, DAGViolation{
			Type:    "ticket_cycle",
			Message: fmt.Sprintf("ticket DAG cycle detected: %v", cycle),
		})
	}

	modCycles := dagTarjanModules(m)
	for _, cycle := range modCycles {
		result.Violations = append(result.Violations, DAGViolation{
			Type:    "module_cycle",
			Message: fmt.Sprintf("module DAG cycle detected: %v", cycle),
		})
	}

	idSet := make(map[string]bool)
	for _, t := range m.Tickets {
		idSet[t.ID] = true
	}
	for _, t := range m.Tickets {
		for _, dep := range t.DependsOn {
			if !idSet[dep] {
				result.Violations = append(result.Violations, DAGViolation{
					Type:    "unknown_dep",
					Message: fmt.Sprintf("%s: depends_on %q is not a known ticket id", t.ID, dep),
				})
			}
		}
	}

	reachable := dagBuildReachability(m)
	fileOwners := make(map[string][]string)
	for _, t := range m.Tickets {
		for _, f := range t.TargetFiles {
			fileOwners[f] = append(fileOwners[f], t.ID)
		}
	}
	for _, owners := range fileOwners {
		if len(owners) < 2 {
			continue
		}
		for i := 0; i < len(owners); i++ {
			for j := i + 1; j < len(owners); j++ {
				a, b := owners[i], owners[j]
				if reachable[a][b] || reachable[b][a] {
					continue
				}
				shared := dagSharedFile(a, b, m)
				result.Violations = append(result.Violations, DAGViolation{
					Type:    "target_file_conflict",
					Message: fmt.Sprintf("%s and %s share target_file %q without a depends_on edge", a, b, shared),
				})
			}
		}
	}

	result.CriticalPath = dagCriticalPath(m)
	return result
}

// dagTarjanTickets runs Tarjan's SCC algorithm on the ticket depends_on graph.
// Returns SCCs with size > 1 (cycles) and self-loops.
func dagTarjanTickets(m *Manifest) [][]string {
	idx := 0
	stack := []string{}
	onStack := make(map[string]bool)
	indices := make(map[string]int)
	lowlink := make(map[string]int)

	adj := make(map[string][]string)
	for _, t := range m.Tickets {
		adj[t.ID] = t.DependsOn
	}

	var cycles [][]string

	var strongconnect func(v string)
	strongconnect = func(v string) {
		indices[v] = idx
		lowlink[v] = idx
		idx++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range adj[v] {
			if w == v {
				cycles = append(cycles, []string{v})
				continue
			}
			if _, ok := indices[w]; !ok {
				strongconnect(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] {
				if indices[w] < lowlink[v] {
					lowlink[v] = indices[w]
				}
			}
		}

		if lowlink[v] == indices[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			if len(scc) > 1 {
				cycles = append(cycles, scc)
			}
		}
	}

	for _, t := range m.Tickets {
		if _, ok := indices[t.ID]; !ok {
			strongconnect(t.ID)
		}
	}
	return cycles
}

// dagTarjanModules runs Tarjan's SCC algorithm on the module depends_on_modules graph.
func dagTarjanModules(m *Manifest) [][]string {
	idx := 0
	stack := []string{}
	onStack := make(map[string]bool)
	indices := make(map[string]int)
	lowlink := make(map[string]int)

	adj := make(map[string][]string)
	for _, mod := range m.Modules {
		adj[mod.ID] = mod.DependsOnModules
	}

	var cycles [][]string

	var strongconnect func(v string)
	strongconnect = func(v string) {
		indices[v] = idx
		lowlink[v] = idx
		idx++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range adj[v] {
			if w == v {
				cycles = append(cycles, []string{v})
				continue
			}
			if _, ok := indices[w]; !ok {
				strongconnect(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] {
				if indices[w] < lowlink[v] {
					lowlink[v] = indices[w]
				}
			}
		}

		if lowlink[v] == indices[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			if len(scc) > 1 {
				cycles = append(cycles, scc)
			}
		}
	}

	for _, mod := range m.Modules {
		if _, ok := indices[mod.ID]; !ok {
			strongconnect(mod.ID)
		}
	}
	return cycles
}

// dagBuildReachability computes the transitive closure of the ticket depends_on
// graph using Floyd-Warshall. Returns a map from ticket ID to set of reachable
// ticket IDs.
func dagBuildReachability(m *Manifest) map[string]map[string]bool {
	ids := make([]string, len(m.Tickets))
	idIdx := make(map[string]int)
	for i, t := range m.Tickets {
		ids[i] = t.ID
		idIdx[t.ID] = i
	}
	n := len(ids)

	reach := make([][]bool, n)
	for i := 0; i < n; i++ {
		reach[i] = make([]bool, n)
		reach[i][i] = true
	}

	for _, t := range m.Tickets {
		src := idIdx[t.ID]
		for _, dep := range t.DependsOn {
			if dst, ok := idIdx[dep]; ok {
				reach[src][dst] = true
			}
		}
	}

	for k := 0; k < n; k++ {
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if reach[i][k] && reach[k][j] {
					reach[i][j] = true
				}
			}
		}
	}

	result := make(map[string]map[string]bool, n)
	for i, id := range ids {
		result[id] = make(map[string]bool, n)
		for j := 0; j < n; j++ {
			result[id][ids[j]] = reach[i][j]
		}
	}
	return result
}

func dagSharedFile(a, b string, m *Manifest) string {
	af := make(map[string]bool)
	for _, t := range m.Tickets {
		if t.ID == a {
			for _, f := range t.TargetFiles {
				af[f] = true
			}
		}
	}
	for _, t := range m.Tickets {
		if t.ID == b {
			for _, f := range t.TargetFiles {
				if af[f] {
					return f
				}
			}
		}
	}
	return "?"
}

// dagCriticalPath returns the length of the longest path in the ticket DAG.
// Each ticket counts as one unit of path length.
func dagCriticalPath(m *Manifest) int {
	topo := dagTopoSort(m)
	if len(topo) == 0 {
		return 0
	}

	dist := make(map[string]int)
	for _, id := range topo {
		dist[id] = 1
	}

	reverseAdj := make(map[string][]string)
	for _, t := range m.Tickets {
		for _, dep := range t.DependsOn {
			reverseAdj[dep] = append(reverseAdj[dep], t.ID)
		}
	}

	maxLen := 1
	for _, id := range topo {
		if dist[id] > maxLen {
			maxLen = dist[id]
		}
		for _, depender := range reverseAdj[id] {
			if dist[id]+1 > dist[depender] {
				dist[depender] = dist[id] + 1
			}
		}
	}
	return maxLen
}

// dagTopoSort returns a topological ordering of ticket IDs using Kahn's algorithm.
func dagTopoSort(m *Manifest) []string {
	inDegree := make(map[string]int)
	adj := make(map[string][]string)
	for _, t := range m.Tickets {
		if _, ok := inDegree[t.ID]; !ok {
			inDegree[t.ID] = 0
		}
		for _, dep := range t.DependsOn {
			adj[dep] = append(adj[dep], t.ID)
			inDegree[t.ID]++
		}
	}

	var q []string
	for id, deg := range inDegree {
		if deg == 0 {
			q = append(q, id)
		}
	}

	var result []string
	for len(q) > 0 {
		u := q[0]
		q = q[1:]
		result = append(result, u)
		for _, v := range adj[u] {
			inDegree[v]--
			if inDegree[v] == 0 {
				q = append(q, v)
			}
		}
	}
	return result
}
