package state

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/hashicorp/consul/consul/structs"
)

func testStateStore(t *testing.T) *StateStore {
	s, err := NewStateStore(os.Stderr)
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if s == nil {
		t.Fatalf("missing state store")
	}
	return s
}

func testRegisterNode(t *testing.T, s *StateStore, idx uint64, nodeID string) {
	node := &structs.Node{Node: nodeID}
	if err := s.EnsureNode(idx, node); err != nil {
		t.Fatalf("err: %s", err)
	}

	tx := s.db.Txn(false)
	defer tx.Abort()
	n, err := tx.First("nodes", "id", nodeID)
	if err != nil {
		t.Fatalf("err: %s", err, n)
	}
	if result, ok := n.(*structs.Node); !ok || result.Node != nodeID {
		t.Fatalf("bad node: %#v", result)
	}
}

func testRegisterService(t *testing.T, s *StateStore, idx uint64, nodeID, serviceID string) {
	svc := &structs.NodeService{
		ID:      serviceID,
		Address: "1.1.1.1",
		Port:    1111,
	}
	if err := s.EnsureService(idx, nodeID, svc); err != nil {
		t.Fatalf("err: %s", err)
	}

	tx := s.db.Txn(false)
	defer tx.Abort()
	service, err := tx.First("services", "id", nodeID, serviceID)
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if result, ok := service.(*structs.ServiceNode); !ok || result.ServiceID != serviceID {
		t.Fatalf("bad service: %#v", result)
	}
}

func testRegisterCheck(t *testing.T, s *StateStore, idx uint64, nodeID, serviceID, checkID string) {
	chk := &structs.HealthCheck{
		Node:      nodeID,
		CheckID:   checkID,
		ServiceID: serviceID,
	}
	if err := s.EnsureCheck(idx, chk); err != nil {
		t.Fatalf("err: %s", err)
	}

	tx := s.db.Txn(false)
	defer tx.Abort()
	c, err := tx.First("checks", "id", nodeID, checkID)
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if result, ok := c.(*structs.HealthCheck); !ok || result.CheckID != checkID {
		t.Fatalf("bad check: %#v", result)
	}
}

func TestStateStore_EnsureNode_GetNode(t *testing.T) {
	s := testStateStore(t)

	// Fetching a non-existent node returns nil
	if node, err := s.GetNode("node1"); node != nil || err != nil {
		t.Fatalf("expected (nil, nil), got: (%#v, %#v)", node, err)
	}

	// Create a node registration request
	in := &structs.Node{
		Node:    "node1",
		Address: "1.1.1.1",
	}

	// Ensure the node is registered in the db
	if err := s.EnsureNode(1, in); err != nil {
		t.Fatalf("err: %s", err)
	}

	// Retrieve the node again
	out, err := s.GetNode("node1")
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	// Correct node was returned
	if out.Node != "node1" || out.Address != "1.1.1.1" {
		t.Fatalf("bad node returned: %#v", out)
	}

	// Indexes are set properly
	if out.CreateIndex != 1 || out.ModifyIndex != 1 {
		t.Fatalf("bad node index: %#v", out)
	}

	// Update the node registration
	in.Address = "1.1.1.2"
	if err := s.EnsureNode(2, in); err != nil {
		t.Fatalf("err: %s", err)
	}

	// Retrieve the node
	out, err = s.GetNode("node1")
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	// Node and indexes were updated
	if out.CreateIndex != 1 || out.ModifyIndex != 2 || out.Address != "1.1.1.2" {
		t.Fatalf("bad: %#v", out)
	}

	// Node upsert is idempotent
	if err := s.EnsureNode(2, in); err != nil {
		t.Fatalf("err: %s", err)
	}
	out, err = s.GetNode("node1")
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if out.Address != "1.1.1.2" || out.CreateIndex != 1 || out.ModifyIndex != 2 {
		t.Fatalf("node was modified: %#v", out)
	}
}

func TestStateStore_GetNodes(t *testing.T) {
	s := testStateStore(t)

	// Create some nodes in the state store
	testRegisterNode(t, s, 0, "node0")
	testRegisterNode(t, s, 1, "node1")
	testRegisterNode(t, s, 2, "node2")

	// Retrieve the nodes
	nodes, err := s.Nodes()
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	// All nodes were returned
	if n := len(nodes); n != 3 {
		t.Fatalf("bad node count: %d", n)
	}

	// Make sure the nodes match
	for i, node := range nodes {
		if node.CreateIndex != uint64(i) || node.ModifyIndex != uint64(i) {
			t.Fatalf("bad node index: %d, %d", node.CreateIndex, node.ModifyIndex)
		}
		name := fmt.Sprintf("node%d", i)
		if node.Node != name {
			t.Fatalf("bad: %#v", node)
		}
	}
}

func TestStateStore_DeleteNode(t *testing.T) {
	s := testStateStore(t)

	// Create a node and register a service and health check with it.
	testRegisterNode(t, s, 0, "node1")
	testRegisterService(t, s, 1, "node1", "service1")
	testRegisterCheck(t, s, 2, "node1", "", "check1")

	// Delete the node
	if err := s.DeleteNode(3, "node1"); err != nil {
		t.Fatalf("err: %s", err)
	}

	// The node was removed
	if n, err := s.GetNode("node1"); err != nil || n != nil {
		t.Fatalf("bad: %#v (err: %#v)", n, err)
	}

	// Associated service was removed. Need to query this directly out of
	// the DB to make sure it is actually gone.
	tx := s.db.Txn(false)
	defer tx.Abort()
	services, err := tx.Get("services", "id", "node1", "service1")
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if s := services.Next(); s != nil {
		t.Fatalf("bad: %#v", s)
	}

	// Associated health check was removed.
	checks, err := tx.Get("checks", "id", "node1", "check1")
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if c := checks.Next(); c != nil {
		t.Fatalf("bad: %#v", c)
	}

	// Indexes were updated.
	for _, tbl := range []string{"nodes", "services"} {
		if idx := s.maxIndex(tbl); idx != 3 {
			t.Fatalf("bad index: %d (%s)", idx, tbl)
		}
	}
}

func TestStateStore_EnsureService_NodeServices(t *testing.T) {
	s := testStateStore(t)

	// Fetching services for a node with none returns nil
	if res, err := s.NodeServices("node1"); err != nil || res != nil {
		t.Fatalf("expected (nil, nil), got: (%#v, %#v)", res, err)
	}

	// Register the nodes
	testRegisterNode(t, s, 0, "node1")
	testRegisterNode(t, s, 1, "node2")

	// Create the service registration
	ns1 := &structs.NodeService{
		ID:      "service1",
		Service: "redis",
		Tags:    []string{"prod"},
		Address: "1.1.1.1",
		Port:    1111,
	}

	// Service successfully registers into the state store
	if err := s.EnsureService(10, "node1", ns1); err != nil {
		t.Fatalf("err: %s", err)
	}

	// Register a similar service against both nodes
	ns2 := *ns1
	ns2.ID = "service2"
	for _, n := range []string{"node1", "node2"} {
		if err := s.EnsureService(20, n, &ns2); err != nil {
			t.Fatalf("err: %s", err)
		}
	}

	// Register a different service on the bad node
	ns3 := *ns1
	ns3.ID = "service3"
	if err := s.EnsureService(30, "node2", &ns3); err != nil {
		t.Fatalf("err: %s", err)
	}

	// Retrieve the services
	out, err := s.NodeServices("node1")
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	// Only the services for the requested node are returned
	if out == nil || len(out.Services) != 2 {
		t.Fatalf("bad services: %#v", out)
	}

	// Results match the inserted services and have the proper indexes set
	expect1 := *ns1
	expect1.CreateIndex, expect1.ModifyIndex = 10, 10
	if svc := out.Services["service1"]; !reflect.DeepEqual(&expect1, svc) {
		t.Fatalf("bad: %#v", svc)
	}

	expect2 := ns2
	expect2.CreateIndex, expect2.ModifyIndex = 20, 20
	if svc := out.Services["service2"]; !reflect.DeepEqual(&expect2, svc) {
		t.Fatalf("bad: %#v %#v", ns2, svc)
	}

	// Lastly, ensure that the highest index was preserved.
	if out.CreateIndex != 20 || out.ModifyIndex != 20 {
		t.Fatalf("bad index: %d, %d", out.CreateIndex, out.ModifyIndex)
	}
}

func TestStateStore_DeleteNodeService(t *testing.T) {
	s := testStateStore(t)

	// Register a node with one service
	testRegisterNode(t, s, 1, "node1")
	testRegisterService(t, s, 2, "node1", "service1")

	// The service exists
	ns, err := s.NodeServices("node1")
	if err != nil || ns == nil || len(ns.Services) != 1 {
		t.Fatalf("bad: %#v (err: %#v)", ns, err)
	}

	// Register a check with the service
	testRegisterCheck(t, s, 3, "node1", "service1", "check1")

	// Delete the service
	if err := s.DeleteNodeService(4, "node1", "service1"); err != nil {
		t.Fatalf("err: %s", err)
	}

	// The service and check don't exist and the index was updated
	ns, err = s.NodeServices("node1")
	if err != nil || ns == nil || len(ns.Services) != 0 {
		t.Fatalf("bad: %#v (err: %#v)", ns, err)
	}
	checks, err := s.NodeChecks("node1")
	if err != nil || len(checks) != 0 {
		t.Fatalf("bad: %#v (err: %s)", checks, err)
	}
	if idx := s.maxIndex("services"); idx != 4 {
		t.Fatalf("bad index: %d", idx)
	}
}

func TestStateStore_EnsureCheck(t *testing.T) {
	s := testStateStore(t)

	// Create a check associated with the node
	check := &structs.HealthCheck{
		Node:        "node1",
		CheckID:     "check1",
		Name:        "redis check",
		Status:      structs.HealthPassing,
		Notes:       "test check",
		Output:      "aaa",
		ServiceID:   "service1",
		ServiceName: "redis",
	}

	// Creating a check without a node returns error
	if err := s.EnsureCheck(1, check); err != ErrMissingNode {
		t.Fatalf("expected %#v, got: %#v", ErrMissingNode, err)
	}

	// Register the node
	testRegisterNode(t, s, 1, "node1")

	// Creating a check with a bad services returns error
	if err := s.EnsureCheck(1, check); err != ErrMissingService {
		t.Fatalf("expected: %#v, got: %#v", ErrMissingService, err)
	}

	// Register the service
	testRegisterService(t, s, 2, "node1", "service1")

	// Inserting the check with the prerequisites succeeds
	if err := s.EnsureCheck(3, check); err != nil {
		t.Fatalf("err: %s", err)
	}

	// Retrieve the check and make sure it matches
	checks, err := s.NodeChecks("node1")
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if len(checks) != 1 {
		t.Fatalf("wrong number of checks: %d", len(checks))
	}
	if !reflect.DeepEqual(checks[0], check) {
		t.Fatalf("bad: %#v", checks[0])
	}

	// Modify the health check
	check.Output = "bbb"
	if err := s.EnsureCheck(4, check); err != nil {
		t.Fatalf("err: %s", err)
	}

	// Check that we successfully updated
	checks, err = s.NodeChecks("node1")
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if len(checks) != 1 {
		t.Fatalf("wrong number of checks: %d", len(checks))
	}
	if checks[0].Output != "bbb" {
		t.Fatalf("wrong check output: %#v", checks[0])
	}
	if checks[0].CreateIndex != 3 || checks[0].ModifyIndex != 4 {
		t.Fatalf("bad index: %#v", checks[0])
	}
}

func TestStateStore_DeleteCheck(t *testing.T) {
	s := testStateStore(t)

	// Register a node and a node-level health check
	testRegisterNode(t, s, 1, "node1")
	testRegisterCheck(t, s, 2, "node1", "", "check1")

	// Delete the check
	if err := s.DeleteCheck(3, "node1", "check1"); err != nil {
		t.Fatalf("err: %s", err)
	}

	// Check is gone
	checks, err := s.NodeChecks("node1")
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if len(checks) != 0 {
		t.Fatalf("bad: %#v", checks)
	}
	if n := s.maxIndex("checks"); n != 3 {
		t.Fatalf("bad index: %d", n)
	}
}