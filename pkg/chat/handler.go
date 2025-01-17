package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/AsterNighT/software-engineering-backend/api"
	"github.com/AsterNighT/software-engineering-backend/pkg/database/models"
	"github.com/AsterNighT/software-engineering-backend/pkg/utils"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

type RoleType = int

const (
	Doctor  RoleType = 1
	Patient RoleType = 2
)

type ClientMsgType = int

const (
	MsgFromClient        ClientMsgType = 1
	CloseChat            ClientMsgType = 2
	RequireMedicalRecord ClientMsgType = 3
	RequirePrescription  ClientMsgType = 4
	RequireQuestions     ClientMsgType = 5
)

type ServerMsgType int

const (
	NewChat           ServerMsgType = 6
	MsgFromServer     ServerMsgType = 7
	SendMedicalRecord ServerMsgType = 8
	SendPrescription  ServerMsgType = 9
	SendQuestions     ServerMsgType = 10
	ChatTerminated    ServerMsgType = 11
)

type Client struct {
	ID        uint
	Role      RoleType
	Conn      *websocket.Conn
	MsgBuffer chan []byte //Message to send to this client
}

type InfoClient struct {
	ID   uint
	Role RoleType
}

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		// deal with cross field problem
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	Clients = make(map[uint]*Client)
	//Clients  = make(map[*Client]bool)
	Connections = make(map[uint](map[uint]bool))
	//Connections = make(map[*Client][]*Client)
	recordServerAddr = "https://neon-cubes.xyz:5000/record_p/"
)

//Add a new client into pool
func AddClient(client *Client, c echo.Context) {
	fmt.Printf("ChatServer$ AddClient(): clientID: %d\n", client.ID)

	c.Logger().Debug("ChatServer$: New client connected")
	Clients[client.ID] = client
	fmt.Printf("ChatServer$ AddClient(): Clients number: %d\n", len(Clients))

	//for test

	// if len(Clients) == 2 {
	// 	StartNewChat(111, 222, c)
	// }
	// if len(Clients) == 3 {
	// 	StartNewChat(111, 333, c)
	// }
}

//Delete a client from pool
func DeleteClient(client *Client, c echo.Context) {
	c.Logger().Debug("ChatServer$: Client disconnected")
	client.Conn.Close()
	close(client.MsgBuffer)
	delete(Clients, client.ID)
	if _, ok := Connections[client.ID]; ok {
		connMap := Connections[client.ID]
		for senderID := range connMap { //search the map result for receiver
			receiverMap := Connections[senderID]
			delete(receiverMap, client.ID)
			Connections[senderID] = receiverMap
		}
	}
	delete(Connections, client.ID)
	fmt.Printf("ChatServer$ DeleteClient(): clientID: %d\n", client.ID)
	fmt.Printf("ChatServer$ DeleteClient(): Clients number: %d\n", len(Clients))
	fmt.Printf("ChatServer$ DeleteClient(): Connections number: %d\n", len(Connections))
}

type ChatHandler struct {
}

// @Summary create a new connection for patient
// @Description
// @Tags Chat
// @Produce json
// @Param newClient body Client true "id, role, conn and msgbuffer"
// @Success 200 {object} api.ReturnedData{}
// @Router /patient/{patientID}/chat [POST]
func (h *ChatHandler) NewPatientConn(c echo.Context) error {

	fmt.Println("ChatServer$: NewPatientConn()")

	//return c.JSON(400, api.Return("Patient Upgrade Fail", nil))
	c.Logger().Debug("ChatServer$: NewPatientConn()")
	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		c.Logger().Debug(err)
		return c.JSON(400, api.Return("Upgrade Fail", nil))
	}

	patientID, err := strconv.Atoi(c.Param("patientID"))
	if err != nil {
		c.Logger().Debug(err)
		return c.JSON(400, api.Return("Invalid ID", nil))
	}
	newClient := &Client{
		ID:        uint(patientID),
		Role:      Patient,
		Conn:      conn,
		MsgBuffer: make(chan []byte),
	}
	go newClient.Read(c)
	go newClient.Send(c)

	AddClient(newClient, c)

	return c.JSON(200, api.Return("ok", nil))
}

// @Summary create a new connection for doctor
// @Description
// @Tags Chat
// @Produce json
// @Param newClient body Client true "id, role, conn and msgbuffer"
// @Success 200 {object} api.ReturnedData{}
// @Router /doctor/{doctorID}/chat [POST]
func (h *ChatHandler) NewDoctorConn(c echo.Context) error {

	fmt.Println("ChatServer$: NewDoctorConn()")
	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		c.Logger().Debug(err)
		return c.JSON(400, api.Return("Upgrade Fail", nil))
	}

	doctorID, err := strconv.Atoi(c.Param("doctorID"))
	if err != nil {
		c.Logger().Debug(err)
		return c.JSON(400, api.Return("Invalid ID", nil))
	}
	newClient := &Client{
		ID:        uint(doctorID),
		Role:      Doctor,
		Conn:      conn,
		MsgBuffer: make(chan []byte),
	}
	//Add client to database
	go newClient.Read(c)
	go newClient.Send(c)

	AddClient(newClient, c)

	return c.JSON(200, api.Return("ok", nil))
}

func (client *Client) FindReceiver(message *Message, c echo.Context) *Client {

	var receiver *Client
	receiverMap, ok := Connections[client.ID]
	if !ok {
		client.ReceiverInvalid(message, c)
	}

	if _, ok = receiverMap[message.ReceiverID]; !ok {
		client.ReceiverInvalid(message, c)
	}

	receiver, ok = Clients[message.ReceiverID]
	if !ok {
		client.ReceiverNotConnected(message, c)
	}

	return receiver

}

func (client *Client) FindPatient(message *Message, c echo.Context) *Client {

	var receiver *Client
	receiverMap, ok := Connections[client.ID]
	if !ok {
		client.ReceiverInvalid(message, c)
	}

	if _, ok = receiverMap[message.PatientID]; !ok {
		client.ReceiverInvalid(message, c)
	}

	receiver, ok = Clients[message.PatientID]
	if !ok {
		client.ReceiverNotConnected(message, c)
	}

	return receiver

}

func StartNewChat(doctorID uint, patientID uint, c echo.Context) error {

	//Find doctor and patient in Clients[]

	if _, ok := Clients[doctorID]; !ok {
		ClientNotConnected(doctorID, Doctor, c)
		return errors.New("client not connected")
	}
	if _, ok := Clients[patientID]; !ok {
		ClientNotConnected(patientID, Patient, c)
		return errors.New("client not connected")
	}

	var doctor = Clients[doctorID]
	var patient = Clients[patientID]

	//look up doctor in Connections
	if _, ok := Connections[doctor.ID]; !ok { //map result doesn't exist
		receiverMap := make(map[uint]bool)
		receiverMap[patient.ID] = true
		Connections[doctor.ID] = receiverMap
	} else { //map result exists
		receiverMap := Connections[doctor.ID]
		receiverMap[patient.ID] = true
		Connections[doctor.ID] = receiverMap //add receiver to map result
	}

	//look up patient in Connections
	if _, ok := Connections[patient.ID]; !ok { //map result doesn't exist
		receiverMap := make(map[uint]bool)
		receiverMap[doctor.ID] = true
		Connections[patient.ID] = receiverMap
	} else { //map result exists
		receiverMap := Connections[patient.ID]
		receiverMap[doctor.ID] = true
		Connections[patient.ID] = receiverMap //add receiver to map result
	}

	//use db to find doctor and patient's names
	db := utils.GetDB()
	var doc models.Account
	db.Where("id = ?", doctor.ID).Find(&doc)
	var pat models.Account
	db.Where("id = ?", patient.ID).Find(&pat)

	//send NewChat pkg to both doctor and patient
	msg := Message{
		Type:        int(NewChat),
		PatientID:   patient.ID,
		DoctorID:    doctor.ID,
		DoctorName:  doc.FirstName + doc.LastName, //docAccount.FirstName + docAccount.LastName,
		PatientName: pat.FirstName + doc.LastName, //patAccount.FirstName + patAccount.LastName,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	doctor.MsgBuffer <- msgBytes
	patient.MsgBuffer <- msgBytes

	return nil
}

//Read subroutine for client
func (client *Client) Read(c echo.Context) {
	defer func() { //delete the client from pool
		//delete client from database
		if Clients[client.ID] != nil {
			DeleteClient(client, c)
		}
	}()

	for {
		_, message, err := client.Conn.ReadMessage()
		if err != nil {
			c.Logger().Debug("ChatServer$: Read():" + err.Error())
			break
		}
		fmt.Println("ChatServer$: " + string(message))
		client.ProcessMessage(message, c)
	}
}

//Send subroutine for client
func (client *Client) Send(c echo.Context) {
	for {
		message, ok := <-client.MsgBuffer
		if !ok {
			//err := client.Conn.WriteMessage(websocket.CloseMessage, []byte{})
			// if err != nil {
			// 	c.Logger().Debug("ChatServer$: Send: " + err.Error())
			// 	return
			// }
			return
		}

		err := client.Conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			c.Logger().Debug("ChatServer$: Send: " + err.Error())
			return
		}
	}
}

type Message struct {
	Type        int    `json:"Type"`
	Role        int    `json:"Role,omitempty"`
	PatientName string `json:"PatientName,omitempty"`
	DoctorName  string `json:"DoctorName,omitempty"`
	SenderID    uint   `json:"SenderID,omitempty"`
	ReceiverID  uint   `json:"ReceiverID,omitempty"`
	PatientID   uint   `json:"PatientID,omitempty"`
	DoctorID    uint   `json:"DoctorID,omitempty"`
	Content     string `json:"Content,omitempty"`
	Time        string `json:"Time,omitempty"`
	CaseID      string `json:"CaseID,omitempty"`
	URL         string `json:"Url,omitempty"`
	Questions   string `json:"Questions,omitempty"`
}

//Process one message
func (client *Client) ProcessMessage(msgBytes []byte, c echo.Context) {
	message := &Message{}
	err := json.Unmarshal(msgBytes, message)
	if err != nil {
		c.Logger().Debug("ChatServer$: ProcessMessage: " + err.Error())
		return
	}
	switch message.Type {
	case MsgFromClient:
		client.MsgFromClient(message, c)
	case CloseChat:
		client.CloseChat(message, c)
	case RequireMedicalRecord:
		client.RequireMedicalRecord(message, c)
	case RequirePrescription:
		client.RequirePrescription(message, c)
	case RequireQuestions:
		client.RequireQuestions(message, c)
	default:
		client.WrongMsgType(message, c)
	}
}

//Process msgfromclient message
func (client *Client) MsgFromClient(message *Message, c echo.Context) {
	receiver := client.FindReceiver(message, c)
	if receiver == nil {
		client.ReceiverNotConnected(message, c)
		return
	}
	fmt.Printf("ChatServer:$ ReceiverID: %d\n", receiver.ID)

	msg := Message{
		Type:       int(MsgFromServer),
		SenderID:   message.SenderID,
		ReceiverID: message.ReceiverID,
		Content:    message.Content,
		Time:       message.Time,
	}
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		c.Logger().Debug("ChatServer$: MsgFromClient: " + err.Error())
		return
	}
	receiver.MsgBuffer <- msgBytes //add the message to receiver buffer
}

//Process closechat message
func (client *Client) CloseChat(message *Message, c echo.Context) {
	receiver := client.FindReceiver(message, c)
	if receiver == nil {
		client.ReceiverNotConnected(message, c)
		return
	}

	msg := Message{
		Type: int(ChatTerminated),
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		c.Logger().Debug("ChatServer$: CloseChat: " + err.Error())
		return
	}

	receiver.MsgBuffer <- msgBytes
}

//Process requiremedicalrecord message
func (client *Client) RequireMedicalRecord(message *Message, c echo.Context) {
	receiver := client.FindPatient(message, c)
	if receiver == nil {
		client.ReceiverNotConnected(message, c)
		return
	}

	//find case id from database
	db := utils.GetDB()
	var case1 models.Case
	db.Where("patient_id = ?", message.PatientID).Order("date DESC").Limit(1).First(&case1)

	msg := Message{
		Type:      int(SendMedicalRecord),
		PatientID: message.PatientID,
		DoctorID:  message.DoctorID,
		URL:       recordServerAddr + strconv.Itoa(int(message.PatientID)) + "/" + strconv.Itoa(int(case1.ID)), // get from database
	}
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		c.Logger().Debug("ChatServer$: RequireMedicalRecord: " + err.Error())
		return
	}
	client.MsgBuffer <- msgBytes //add the message to sender buffer
	receiver.MsgBuffer <- msgBytes
}

//Process requireprescription message
func (client *Client) RequirePrescription(message *Message, c echo.Context) {
	//db := utils.GetDB()
	//var pres []cases.Prescription
	//db.Where("CaseID = ?", message.CaseID).Find(&pres)
	receiver := client.FindPatient(message, c)
	if receiver == nil {
		client.ReceiverNotConnected(message, c)
		return
	}
	msg := Message{
		Type:      int(SendPrescription),
		PatientID: message.PatientID,
		DoctorID:  message.DoctorID,
		URL:       "PRESCRIPTION url", //TODO how to convert
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		c.Logger().Debug("ChatServer$: RequirePrescription: " + err.Error())
		return
	}
	client.MsgBuffer <- msgBytes //add the message to sender buffer
	receiver.MsgBuffer <- msgBytes
}

//Process requirequestions message
func (client *Client) RequireQuestions(message *Message, c echo.Context) {
	db := utils.GetDB()
	var doc models.Doctor
	db.Where("account_id = ?", client.ID).Find(&doc)
	var depart models.Department
	db.Where("id = ?", doc.Department).Find(&depart)
	fmt.Printf("ChatServer:$ Questions: %s\n", depart.Questions)
	msg := Message{
		Type:      int(SendQuestions),
		Questions: depart.Questions,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		c.Logger().Debug("ChatServer$: RequireQuestions: " + err.Error())
		return
	}
	client.MsgBuffer <- msgBytes //add the message to sender buffer
}

//Deal with unknown message type
func (client *Client) WrongMsgType(message *Message, c echo.Context) {
	c.Logger().Debug("ChatServer$: WrongMsgType")
}

//Deal with the case when receiver of the message has't connected to the server
func (client *Client) ReceiverNotConnected(message *Message, c echo.Context) {
	c.Logger().Debug("ChatServer$: ReceiverNotConnected")
}

func ClientNotConnected(clientID uint, role RoleType, c echo.Context) {
	c.Logger().Debug("client not connected")
}

func (client *Client) ReceiverInvalid(message *Message, c echo.Context) {
	c.Logger().Debug("receiver invalid")
}
