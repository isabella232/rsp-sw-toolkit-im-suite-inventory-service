package tagprocessor

import (
	"fmt"
	"testing"
)

func TestMinRssiFilter(t *testing.T) {
	ds := newTestDataset(2)

	back := generateTestSensor(backStock, NoPersonality)

	// set the minimum rssi to arbitrary value
	back.minRssiDbm10X = -600

	// tag with good rssi
	ds.readTag(0, back, -580, 1)
	// tag with bad rssi
	ds.readTag(1, back, -620, 1)

	ds.updateTagRefs()

	if err := ds.verifyTag(0, Present, back); err != nil {
		t.Error(err)
	}
	// tag1 will NOT arrive due to having an rssi too low
	if ds.tags[1] != nil {
		t.Errorf("expected tag with index 1 to be nil, but was %#v", ds.tags[1])
	}

	// todo: check for 1 arrival events
}

func TestPosDoesNotGenerateArrival(t *testing.T) {
	ds := newTestDataset(10)

	front := generateTestSensor(salesFloor, NoPersonality)
	posSensor := generateTestSensor(salesFloor, POS)

	ds.readAll(posSensor, rssiMin, 1)
	ds.updateTagRefs()
	if err := ds.verifyAll(Unknown, posSensor); err != nil {
		t.Error(err)
	}
	// todo: ensure NO arrivals were generated

	// read a few more times, we still do not want to arrive
	ds.readAll(posSensor, rssiMin, 4)
	if err := ds.verifyAll(Unknown, posSensor); err != nil {
		t.Error(err)
	}
	// todo: ensure NO arrivals were generated

	ds.readAll(front, rssiStrong, 1)
	// tags will have arrived now, but will still be in the location of the pos sensor
	if err := ds.verifyAll(Present, posSensor); err != nil {
		t.Error(err)
	}
	// todo: ensure ALL arrivals WERE generated

}

func TestBasicArrival(t *testing.T) {
	ds := newTestDataset(10)

	front := generateTestSensor(salesFloor, NoPersonality)

	ds.readAll(front, rssiWeak, 1)
	ds.updateTagRefs()

	if err := ds.verifyAll(Present, front); err != nil {
		t.Error(err)
	}

	// todo: check for 1 arrival events
}

func TestTagMoveWeakRssi(t *testing.T) {
	ds := newTestDataset(10)

	back1 := generateTestSensor(backStock, NoPersonality)
	back2 := generateTestSensor(backStock, NoPersonality)
	back3 := generateTestSensor(backStock, NoPersonality)

	// start all tags in the back stock
	ds.readAll(back1, rssiMin, 1)
	ds.updateTagRefs()
	if err := ds.verifyAll(Present, back1); err != nil {
		t.Error(err)
	}

	// move tags to same facility, different sensor
	ds.readAll(back2, rssiStrong, 4)
	if err := ds.verifyAll(Present, back2); err != nil {
		t.Error(err)
	}

	// test that tag stays at new location even with concurrent reads from weaker sensor
	// MOVE back doesn't happen with weak RSSI
	ds.readAll(back3, rssiWeak, 1)
	if err := ds.verifyAll(Present, back2); err != nil {
		t.Error(err)
	}

	// todo: check for events
}

func TestMoveAntennaLocation(t *testing.T) {
	antennaIds := []int{1, 4, 33, 15, 99}

	sensor := generateTestSensor(backStock, NoPersonality)

	for _, antId := range antennaIds {
		t.Run(fmt.Sprintf("Antenna-%d", antId), func(t *testing.T) {
			ds := newTestDataset(1)

			// start all tags at antenna port 0
			ds.readAll(sensor, rssiMin, 1)
			ds.updateTagRefs()

			// move tag to a different antenna port on same sensor
			ds.tagReads[0].AntennaId = antId
			ds.readTag(0, sensor, rssiStrong, 4)
			if ds.tags[0].Location != sensor.getAntennaAlias(antId) {
				t.Errorf("tag location was %s, but we expected %s.\n\t%#v",
					ds.tags[0].Location, sensor.getAntennaAlias(antId), ds.tags[0])
			}
		})
	}
}

func TestMoveSameFacility(t *testing.T) {
	ds := newTestDataset(10)

	back1 := generateTestSensor(backStock, NoPersonality)
	back2 := generateTestSensor(backStock, NoPersonality)

	// start all tags in the back stock
	ds.readAll(back1, rssiMin, 1)
	ds.updateTagRefs()
	if err := ds.verifyAll(Present, back1); err != nil {
		t.Error(err)
	}

	// move tag to same facility, different sensor
	ds.readAll(back2, rssiStrong, 4)
	if err := ds.verifyAll(Present, back2); err != nil {
		t.Error(err)
	}
	// todo: check for events
}

func TestMoveDifferentFacility(t *testing.T) {
	ds := newTestDataset(10)

	front := generateTestSensor(salesFloor, NoPersonality)
	back := generateTestSensor(backStock, NoPersonality)

	// start all tags in the front sales floor
	ds.readAll(front, rssiMin, 1)
	ds.updateTagRefs()
	if err := ds.verifyAll(Present, front); err != nil {
		t.Error(err)
	}

	// move tag to different facility
	ds.readAll(back, rssiStrong, 4)
	if err := ds.verifyAll(Present, back); err != nil {
		t.Error(err)
	}

	// todo: check for events
	// expect depart and arrival
}

func TestBasicExit(t *testing.T) {
	ds := newTestDataset(9)

	back := generateTestSensor(backStock, NoPersonality)
	frontExit := generateTestSensor(salesFloor, Exit)
	front := generateTestSensor(salesFloor, NoPersonality)

	// get it in the system
	ds.readAll(back, rssiMin, 4)
	ds.updateTagRefs()

	// one tag read by an EXIT will not make the tag go exiting.
	ds.readAll(frontExit, rssiMin, 1)
	if err := ds.verifyAll(Present, back); err != nil {
		t.Error(err)
	}

	// moving to an exit sensor will put tag in exiting
	// moving to an exit sensor in another facility will generate departure / arrival
	ds.readAll(frontExit, rssiWeak, 10)
	if err := ds.verifyAll(Exiting, frontExit); err != nil {
		t.Error(err)
	}
	// todo: need to check for events that were generated

	// clear exiting by moving to another sensor
	// done in a loop to simulate being read simultaneously, not 20 on one sensor, and 20 on another
	for i := 0; i < 20; i++ {
		ds.readAll(frontExit, rssiMin, 1)
		ds.readAll(front, rssiStrong, 1)
	}
	if err := ds.verifyAll(Present, front); err != nil {
		t.Error(err)
	}

	ds.readAll(frontExit, rssiMax, 20)
	if err := ds.verifyAll(Exiting, frontExit); err != nil {
		t.Error(err)
	}
}

func TestExitingArrivalDepartures(t *testing.T) {
	ds := newTestDataset(5)

	back := generateTestSensor(backStock, NoPersonality)
	frontExit := generateTestSensor(salesFloor, Exit)
	front := generateTestSensor(salesFloor, NoPersonality)

	ds.readAll(back, rssiMin, 4)

	ds.updateTagRefs()
	if err := ds.verifyAll(Present, back); err != nil {
		t.Error(err)
	}

	// one tag read by an EXIT will not make the tag go exiting.
	ds.readAll(frontExit, rssiWeak, 1)
	if err := ds.verifyAll(Present, back); err != nil {
		t.Error(err)
	}

	// go to exiting state
	ds.readAll(frontExit, rssiWeak, 10)
	if err := ds.verifyAll(Exiting, frontExit); err != nil {
		t.Error(err)
	}

	// clear exiting by moving to another sensor
	ds.readAll(frontExit, rssiMin, 20)
	ds.readAll(front, rssiStrong, 20)
	if err := ds.verifyAll(Present, front); err != nil {
		t.Error(err)
	}

	// go exiting again
	ds.readAll(frontExit, rssiMax, 20)
	if err := ds.verifyAll(Exiting, frontExit); err != nil {
		t.Error(err)
	}

	// todo: need to check the events have been generated??
}

func TestTagDepartAndReturnFromExit(t *testing.T) {

}

func TestTagDepartAndReturnPOS(t *testing.T) {

}
