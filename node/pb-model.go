package main

import (
	"encoding/json"
	"fmt"
	"log"
)

// A node represents an AP, device or other transmitting/recieving address including broadcast
type Node struct {
	// Adjacency information
	associations []*Node

	// Node data
	knownAs              string
	ssid                 string
	bssid                []string
	nodeType             string
	transmitterAddresses []string
	timesSeen            int
	strength             []int8
	seen                 []string
	firstSeen            string
}

type NodeList struct {
	nodes map[string]*Node
}

func newGraph(debugOn bool) NodeList {

	if debugOn {
		log.Printf("DEBUG: Creating Graph model\n")
	}

	return NodeList{
		nodes: make(map[string]*Node),
	}
}

// Beacon Packets all originate from a broadcasting access point.  Associations do not exist since they are advertising packets.
// Function takes a packet and then creates a new node.  The new node then either updates an existing or creates a new node.
func addNodeFromBeacon(graph *NodeList, inNode *BeaconNode, debugOn bool) bool {

	// In all cases create a new node as a base to work against
	newNode := createNodeFromBeacon(inNode)

	// See if the 'knownAs' value exists in the list of all known nodes
	val, ok := graph.nodes[newNode.knownAs]
	if ok {
		// Found a matching knownAs in the Node List, update values.
		val.timesSeen++
		val.strength = updateBufferedStrength(val.strength, inNode.sigStrength, debugOn)
		val.seen = updateBufferedTimes(val.seen, inNode.timestamp, debugOn)

		if debugOn {
			log.Printf("DEBUG: Updating node %v, seen %v times on %v transmitting addresses\n",
				inNode.ssid,
				val.timesSeen,
				len(val.transmitterAddresses))
		}

	} else { // Not an existing 'knownAs' so we need a new node
		graph.nodes[newNode.knownAs] = &newNode
		val = graph.nodes[newNode.knownAs] // Set val to the newly created Node

		if debugOn {
			log.Printf("DEBUG: New node %v added to Graph List\n", val.knownAs)
		}
	}

	if val.nodeType == "MgmtProbeReq" { // Probe request, update the associations and make sure both ends of the probe exist

		if debugOn {
			log.Printf("DEBUG: Adding associations from probe request\n")
		}

		valAssoc, ok := graph.nodes[newNode.ssid] // Check if we have the node that is being probed

		if !ok { // Create a skeleton endpoint for the probe and add to the Graph List.

			if debugOn {
				log.Printf("DEBUG: Probe request to an undiscovered SSID: %v so adding as new node\n", newNode.ssid)
			}
			assocNode := Node{knownAs: newNode.ssid}
			graph.nodes[assocNode.knownAs] = &assocNode

			valAssoc = graph.nodes[assocNode.knownAs]
		}

		// Add unique probe packet knownAs to the SSID knownAs and vice versa

		// Check if valAssoc is in val.associations and if not then add it
		if !containsAssociation(val, valAssoc) {
			log.Println("*******************Matched Assoc 1")
			val.associations = append(val.associations, valAssoc)
		}

		// Check if val is in valAssoc.associations and if it is not then add it
		if !containsAssociation(valAssoc, val) {
			log.Println("*******************Matched Assoc 2")
			valAssoc.associations = append(valAssoc.associations, val)
		}

		// Remove SSID from the probe target - bit of a hack...
		val.ssid = ""

		if debugOn {
			log.Printf("DEBUG: Added %v to node %v and vice versa\n", valAssoc.knownAs, val.knownAs)
			log.Println("FROM NODE: ")
			PrintNodeDetail(val)
			log.Println("TO NODE: ")
			PrintNodeDetail(valAssoc)

		}
	}

	// Write out to the database folder
	writeToDatabase(val, dbName, debugOn)

	return true
}

// Create a Node from a BeaconNode to then be used to manipulate data into.
func createNodeFromBeacon(beacon *BeaconNode) Node {

	n := Node{}
	n.strength = make([]int8, pBuffer)
	n.seen = make([]string, pBuffer)

	// Data settings based on BeaconProbe type
	switch beacon.ptype {

	case "MgmtProbeReq":

		if debugOn {
			log.Printf("DEBUG: Probe request (%v), setting KnownAs to %v\n", beacon.ptype, beacon.transmitter)
		}

		n.knownAs = beacon.transmitter

	case "MgmtBeacon":

		if debugOn {
			log.Printf("DEBUG: Beacon request (%v), setting KnownAs to %v\n", beacon.ptype, beacon.ssid)
		}

		n.knownAs = beacon.ssid

	default:

		if debugOn {
			log.Printf("DEBUG: Default packet type applied to %v, setting KnownAs to %v\n", beacon.ptype, beacon.ssid)
		}

		n.knownAs = beacon.ssid
	}

	n.ssid = beacon.ssid
	n.bssid = append(n.bssid, beacon.bssid)
	n.nodeType = beacon.ptype
	n.transmitterAddresses = append(n.transmitterAddresses, beacon.transmitter)
	n.timesSeen = 1                                                            // Default is 1, this may increase if it already exists in Node List
	n.strength = updateBufferedStrength(n.strength, beacon.sigStrength, false) // Turn off debug since overly noisy
	n.seen = updateBufferedTimes(n.seen, beacon.timestamp, false)              // Turn off debg since overly noisy
	n.firstSeen = beacon.timestamp

	return n

}

// Stength and Seen need to be fixed length to avoid infinite growth
func updateBufferedStrength(strengths []int8, s int8, debugOn bool) []int8 {

	if debugOn {
		log.Printf("DEBUG: Updating Signal Strength buffer on node, value to add %v\n", s)
		log.Printf("DEBUG: Signal Strength buffer before change: %v\n", strengths)
	}

	// Pop and shift the slice - pop currently disappears - then add the new value to the end
	if len(strengths) > 0 {
		_, strengths = strengths[0], strengths[1:]
		strengths = append(strengths, s)
	} else {
		strengths = append(strengths, s)
	}

	if debugOn {
		log.Printf("DEBUG: Signal Strength buffer after change: %v\n", strengths)
	}

	return strengths

}

// Stength and Seen need to be fixed length to avoid infinite growth
func updateBufferedTimes(times []string, t string, debugOn bool) []string {

	if debugOn {
		log.Printf("DEBUG: Updating Times Seen buffer on node, value to add %v\n", t)
		//log.Printf("DEBUG: Times Seen buffer before change: %v\n", times)
	}

	// Pop and shift the slice - pop currently disappears - then add the new value to the end
	if len(times) > 0 {
		_, times = times[0], times[1:]
		times = append(times, t)
	} else {
		times = append(times, t)
	}

	return times

}

// Check if 'b' Node is in 'a' Node.associations.
func containsAssociation(a *Node, b *Node) bool {
	for _, v := range a.associations {
		if v == b {
			return true
		}
	}
	return false
}

// Marshall out to json and write to the database folder
func writeToDatabase(node *Node, dbName string, debugOn bool) bool {

	if debugOn {
		log.Printf("DEBUG: Writing %v out to database folder %v", node.knownAs, dbName)
	}

	jsonOut, err := json.Marshal(*node)

	if err != nil {
		panic(err)
	}

	fmt.Printf("**************** JSON Data: %v \n", string(jsonOut))
	return true
}
