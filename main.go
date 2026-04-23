package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/thedevsaddam/renderer"
	"go.mongodb.org/mongo-driver/bson"
	b "go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// Creating the important variables
var rnd *renderer.Render
var client *mongo.Client
var db *mongo.Database

const (
	dbName         string = "golang-todo"
	collectionName string = "todo"
)

type ToDoModel struct {
	ID        primitive.ObjectID `bison:"id,omitempty"`
	Title     string             `bison:"title"`
	Completed bool               `bison:"completed"`
	CreatedAt time.Time          `bison:"created_at"`
}

type ToDo struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"created_at"`
}

type GetTodoResponse struct {
	Message string `json:"Message"`
	Data    []ToDo `json:"Data"`
}

type CreateToDo struct {
	Title string `json:"Title"`
}

type UpdateToDo struct {
	Title     string `json:"title"`
	Completed bool   `json:"completed"`
}

func init() {
	fmt.Println("initalizing the db")
	rnd = renderer.New()
	var err error

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err = mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		log.Fatal(err)
	}

	err = client.Ping(ctx, readpref.Primary())
	if err != nil {
		log.Fatal(err)
	}
}

func main() {

	// Create the router
	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Get("/", homeHandler)
	router.Mount("/todo", todoHandlers())

	// Create the server
	server := &http.Server{
		Addr:         ":9000",
		Handler:      router,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	// create a stop channel to recieve the halt
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt)

	// start the server in a separate go routine
	go func() {
		fmt.Printf("Starting the server on port: %v\n", 9000)
		if err := server.ListenAndServe(); err != nil {
			log.Printf("listen: %s\n", err)
		}
	}()

	sig := <-stopChan
	log.Printf("The stop signal was: %v\n", sig)

	// disconnect from the DB
	if err := client.Disconnect(context.Background()); err != nil {
		panic(err)
	}

	// create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// shutdown the server
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown fatal: %v", err)
	}
	fmt.Println("Server shut down gracefully")

}

func homeHandler(rw http.ResponseWriter, r *http.Request) {
	filepath := "./README.md"
	err := rnd.FileView(rw, http.StatusOK, filepath, "readme.md")
	if err != nil {
		log.Fatalf("OH NO! An error: %v", err)
	}
}

func todoHandlers() http.Handler {
	router := chi.NewRouter()

	router.Group(func(r chi.Router) {
		r.Get("/", getToDos)
		r.Post("/", createToDo)
		r.Put("/{id}", updateToDo)
		r.Delete("/{id}", deleteToDo)
	})
	return router
}

func getToDos(rw http.ResponseWriter, r *http.Request) {
	var todoListFromDB = []ToDoModel{}
	filter := b.D{}

	cursor, err := db.Collection(collectionName).Find(context.Background(), filter)
	if err != nil {
		log.Printf("failed to fetch todo records from the db: %v\n", err.Error())
		rnd.JSON(rw, http.StatusBadRequest, renderer.M{
			"message": "Could not fetch the todo list",
			"error":   err.Error(),
		})
	}

	todoList := []ToDo{}
	if err = cursor.All(context.Background(), &todoListFromDB); err != nil {
		log.Fatalf("OH NO! AN error: %v", err)
	}

	for _, td := range todoListFromDB {
		todoList = append(todoList, ToDo{
			ID:        td.ID.Hex(),
			Title:     td.Title,
			Completed: td.Completed,
			CreatedAt: td.CreatedAt,
		})
	}
	rnd.JSON(rw, http.StatusOK, GetTodoResponse{
		Message: "All todo's retrieved",
		Data:    todoList,
	})
}

func createToDo(rw http.ResponseWriter, r *http.Request) {
	var todoReq CreateToDo
	// var todoRequestBody string

	if err := json.NewDecoder(r.Body).Decode(&todoReq); err != nil {
		log.Printf("failed to decode json data: %v", err.Error())
		rnd.JSON(rw, http.StatusBadRequest, renderer.M{
			"message": "please add a title",
		})
		return
	}

	if todoReq.Title == "" {
		log.Println("no title added to response body")
		rnd.JSON(rw, http.StatusBadRequest, renderer.M{
			"message": "please add title",
		})
		return
	}

	todooModel := ToDoModel{
		ID:        primitive.NewObjectID(),
		Title:     todoReq.Title,
		Completed: false,
		CreatedAt: time.Now(),
	}

	data, err := db.Collection(collectionName).InsertOne(r.Context(), todooModel)
	if err != nil {
		log.Printf("failed to insert data into the database: %v\n", err.Error())
		rnd.JSON(rw, http.StatusInternalServerError, renderer.M{
			"message": "failed to insert data into the database",
			"error":   err.Error(),
		})
		return
	}
	rnd.JSON(rw, http.StatusCreated, renderer.M{
		"message": "Todo created successfully",
		"ID":      data.InsertedID,
	})
}

func updateToDo(rw http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))

	res, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		log.Printf("invalid ID: %v\n", err.Error())
	}
	rnd.JSON(rw, http.StatusBadRequest, renderer.M{
		"message": "The id is invalid",
		"error":   err.Error(),
	})

	var updateToDoReq UpdateToDo

	if err := json.NewDecoder(r.Body).Decode(&updateToDoReq); err != nil {
		log.Printf("failed to decode the body: %v", err)
		rnd.JSON(rw, http.StatusBadRequest, err.Error())
	}
	if updateToDoReq.Title == "" {
		rnd.JSON(rw, http.StatusBadRequest, renderer.M{
			"message": "Title cannot be empty",
		})
		return
	}

	// find and update the record
	filter := bson.M{"id": res}
	update := bson.M{"$set": bson.M{"title": updateToDoReq.Title, "completed": updateToDoReq.Completed}}
	data, err := db.Collection(collectionName).UpdateOne(r.Context(), filter, update)

	if err != nil {
		log.Printf("failed to update db collection: %v", err.Error())
		rnd.JSON(rw, http.StatusInternalServerError, renderer.M{
			"message": "Failed to update data in the database",
			"error":   err.Error(),
		})
		return
	}
	rnd.JSON(rw, http.StatusOK, renderer.M{
		"message": "Todo updated successfully",
		"data":    data.ModifiedCount,
	})
}

func deleteToDo(rw http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		log.Printf("invalid id: %v\n", err.Error())
		rnd.JSON(rw, http.StatusBadRequest, err.Error())
		return
	}

	filter := bson.M("id": res)
	if data, err := db.Collection(collectionName).DeleteOne(r.Context(), filter); err != nil {
		log.Printf("Could not delete item from db %v", err.Error())
		rnd.JSON(rw, http.StatusInternalServerError, renderer.M{
			"message": "an error occurred when deleting todo item",
			"error": err.Error(),
		})
	} else {
		rnd.JSON(rw, http.StatusOK, renderer.M{
			"message": "successfully deleted todo",
			"data": data
		})
	}

}
