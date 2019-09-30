package costmodel

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"k8s.io/klog"

	costAnalyzerCloud "github.com/kubecost/cost-model/cloud"
	_ "github.com/lib/pq"
)

const remotePW = "REMOTE_WRITE_PASSWORD"
const sqlAddress = "SQL_ADDRESS"

func getNodeCosts(db *sql.DB) (map[string]*costAnalyzerCloud.Node, error) {

	nodes := make(map[string]*costAnalyzerCloud.Node)

	query := `SELECT name, avg(value),labels->>'instance' AS instance, labels->>'cluster_id' AS clusterid
	FROM metrics
	WHERE (name='node_cpu_hourly_cost' OR name='node_ram_hourly_cost' OR name='node_gpu_hourly_cost')  AND value != 'NaN' AND value != 0
	GROUP BY instance,name,clusterid`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			name      string
			avg       float64
			instance  string
			clusterid string
		)
		if err := rows.Scan(&name, &avg, &instance, &clusterid); err != nil {
			return nil, err
		}
		if data, ok := nodes[instance]; ok {
			if name == "node_cpu_hourly_cost" {
				data.VCPUCost = fmt.Sprintf("%f", avg)
			} else if name == "node_ram_hourly_cost" {
				data.RAMCost = fmt.Sprintf("%f", avg)
			} else if name == "node_gpu_hourly_cost" {
				data.GPUCost = fmt.Sprintf("%f", avg)
			}
		} else {
			nodes[instance] = &costAnalyzerCloud.Node{}
			data := nodes[instance]
			if name == "node_cpu_hourly_cost" {
				data.VCPUCost = fmt.Sprintf("%f", avg)
			} else if name == "node_ram_hourly_cost" {
				data.RAMCost = fmt.Sprintf("%f", avg)
			} else if name == "node_gpu_hourly_cost" {
				data.GPUCost = fmt.Sprintf("%f", avg)
			}
		}

	}

	return nodes, nil
}

func CostDataRangeFromSQL(field string, value string, window string, start string, end string) (map[string]*CostData, error) {
	pw := os.Getenv(remotePW)
	address := os.Getenv(sqlAddress)
	connStr := fmt.Sprintf("postgres://postgres:%s@%s:5432?sslmode=disable", pw, address)
	db, err := sql.Open("postgres", connStr)
	defer db.Close()
	if err != nil {
		return nil, err
	}
	nodes, err := getNodeCosts(db)
	if err != nil {
		return nil, err
	}
	model := make(map[string]*CostData)
	query := `SELECT time_bucket($1, time) AS bucket, name, avg(value),labels->>'container' AS container,labels->>'pod' AS pod,labels->>'namespace' AS namespace, labels->>'instance' AS instance, labels->>'cluster_id' AS clusterid
	FROM metrics
	WHERE (name='container_cpu_allocation') AND
	  time > $2 AND time < $3 AND value != 'NaN'
	GROUP BY container,pod,bucket,namespace,instance,clusterid,name
	ORDER BY container,bucket;
	`
	rows, err := db.Query(query, window, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			bucket    string
			name      string
			sum       float64
			container string
			pod       string
			namespace string
			instance  string
			clusterid string
		)
		if err := rows.Scan(&bucket, &name, &sum, &container, &pod, &namespace, &instance, &clusterid); err != nil {
			return nil, err
		}
		layout := "2006-01-02T15:04:05Z"
		t, err := time.Parse(layout, bucket)
		if err != nil {
			return nil, err
		}

		k := newContainerMetricFromValues(namespace, pod, container, instance)
		key := k.Key()
		allocationVector := &Vector{
			Timestamp: float64(t.Unix()),
			Value:     sum,
		}
		if data, ok := model[key]; ok {
			if name == "container_cpu_allocation" {
				data.CPUAllocation = append(data.CPUAllocation, allocationVector)
			} else if name == "container_memory_allocation_bytes" {
				data.RAMAllocation = append(data.RAMAllocation, allocationVector)
			} else if name == "container_gpu_allocation" {
				data.GPUReq = append(data.GPUReq, allocationVector)
			}
		} else {
			node, ok := nodes[instance]
			if !ok {
				return nil, fmt.Errorf("No node found")
			}
			model[key] = &CostData{
				Name:          container,
				PodName:       pod,
				NodeName:      instance,
				NodeData:      node,
				CPUAllocation: []*Vector{},
				RAMAllocation: []*Vector{},
				GPUReq:        []*Vector{},
				Namespace:     namespace,
				ClusterID:     clusterid,
			}
			data := model[key]
			if name == "container_cpu_allocation" {
				data.CPUAllocation = append(data.CPUAllocation, allocationVector)
			} else if name == "container_memory_allocation_bytes" {
				data.RAMAllocation = append(data.RAMAllocation, allocationVector)
			} else if name == "container_gpu_allocation" {
				data.GPUReq = append(data.GPUReq, allocationVector)
			}
		}
	}
	query = `SELECT time_bucket($1, time) AS bucket, name, avg(value),labels->>'container' AS container,labels->>'pod' AS pod,labels->>'namespace' AS namespace, labels->>'instance' AS instance, labels->>'cluster_id' AS clusterid
	FROM metrics
	WHERE (name='container_memory_allocation_bytes') AND
		time > $2 AND time < $3 AND value != 'NaN'
	GROUP BY container,pod,bucket,namespace,instance,clusterid,name
	ORDER BY container,bucket;
	`
	rows, err = db.Query(query, window, start, end)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			bucket    string
			name      string
			sum       float64
			container string
			pod       string
			namespace string
			instance  string
			clusterid string
		)
		if err := rows.Scan(&bucket, &name, &sum, &container, &pod, &namespace, &instance, &clusterid); err != nil {
			return nil, err
		}
		layout := "2006-01-02T15:04:05Z"
		t, err := time.Parse(layout, bucket)
		if err != nil {
			return nil, err
		}

		k := newContainerMetricFromValues(namespace, pod, container, instance)
		key := k.Key()
		allocationVector := &Vector{
			Timestamp: float64(t.Unix()),
			Value:     sum,
		}
		if data, ok := model[key]; ok {
			if name == "container_cpu_allocation" {
				data.CPUAllocation = append(data.CPUAllocation, allocationVector)
			} else if name == "container_memory_allocation_bytes" {
				data.RAMAllocation = append(data.RAMAllocation, allocationVector)
			} else if name == "container_gpu_allocation" {
				data.GPUReq = append(data.GPUReq, allocationVector)
			}
		} else {
			node, ok := nodes[instance]
			if !ok {
				return nil, fmt.Errorf("No node found")
			}
			model[key] = &CostData{
				Name:          container,
				PodName:       pod,
				NodeName:      instance,
				NodeData:      node,
				CPUAllocation: []*Vector{},
				RAMAllocation: []*Vector{},
				GPUReq:        []*Vector{},
				Namespace:     namespace,
				ClusterID:     clusterid,
			}
			data := model[key]
			if name == "container_cpu_allocation" {
				data.CPUAllocation = append(data.CPUAllocation, allocationVector)
			} else if name == "container_memory_allocation_bytes" {
				data.RAMAllocation = append(data.RAMAllocation, allocationVector)
			} else if name == "container_gpu_allocation" {
				data.GPUReq = append(data.GPUReq, allocationVector)
			}
		}
	}
	query = `SELECT DISTINCT ON (labels->>'namespace') * FROM METRICS WHERE name='kube_namespace_labels' ORDER BY labels->>'namespace',time DESC;`
	rows, err = db.Query(query)
	if err != nil {
		return nil, err
	}
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	rawResult := make([][]byte, len(cols))
	result := make([]string, len(cols))
	dest := make([]interface{}, len(cols)) // A temporary interface{} slice
	for i, _ := range rawResult {
		dest[i] = &rawResult[i] // Put pointers to each string in the interface slice
	}
	nsToLabels := make(map[string]map[string]string)
	for rows.Next() {
		err = rows.Scan(dest...)
		if err != nil {
			return nil, err
		}

		for i, raw := range rawResult {
			if raw == nil {
				result[i] = "\\N"
			} else {
				result[i] = string(raw)
			}
		}

		klog.Infof("%#v\n", result)
		var dat map[string]string
		err := json.Unmarshal([]byte(result[4]), &dat)
		if err != nil {
			return nil, err
		}

		ns, ok := dat["namespace"]
		if !ok {
			return nil, fmt.Errorf("No namespace found")
		}
		nsToLabels[ns] = dat
	}

	for _, cd := range model {
		ns := cd.Namespace
		if labels, ok := nsToLabels[ns]; ok {
			cd.NamespaceLabels = labels
			cd.Labels = labels // TODO: override with podlabels
		}
	}

	return model, nil
}