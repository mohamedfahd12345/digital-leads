package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Product represents a product with its schema
type Product struct {
	ID          string                 `bson:"_id,omitempty" json:"id"`
	Name        string                 `bson:"name" json:"name"`
	Description string                 `bson:"description" json:"description"`
	Schema      map[string]interface{} `bson:"schema" json:"schema"`
	CreatedAt   time.Time              `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time              `bson:"updated_at" json:"updated_at"`
}

// Lead represents a lead with product reference and data
type Lead struct {
	ID        string                 `bson:"_id,omitempty" json:"id"`
	ProductID string                 `bson:"product_id" json:"product_id"`
	Data      map[string]interface{} `bson:"data" json:"data"`
	CreatedAt time.Time              `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time              `bson:"updated_at" json:"updated_at"`
}

// gRPC Request/Response structs
type CreateProductRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Schema      map[string]interface{} `json:"schema"`
}

type ProductResponse struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Schema      map[string]interface{} `json:"schema"`
	CreatedAt   string                 `json:"created_at"`
	UpdatedAt   string                 `json:"updated_at"`
}

type GetProductRequest struct {
	ID string `json:"id"`
}

type UpdateProductRequest struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Schema      map[string]interface{} `json:"schema"`
}

type DeleteProductRequest struct {
	ID string `json:"id"`
}

type CreateLeadRequest struct {
	ProductID string                 `json:"product_id"`
	Data      map[string]interface{} `json:"data"`
}

type LeadResponse struct {
	ID        string                 `json:"id"`
	ProductID string                 `json:"product_id"`
	Data      map[string]interface{} `json:"data"`
	CreatedAt string                 `json:"created_at"`
	UpdatedAt string                 `json:"updated_at"`
}

type GetLeadRequest struct {
	ID string `json:"id"`
}

type UpdateLeadRequest struct {
	ID   string                 `json:"id"`
	Data map[string]interface{} `json:"data"`
}

type DeleteLeadRequest struct {
	ID string `json:"id"`
}

type ListProductsRequest struct {
	Limit  int32 `json:"limit"`
	Offset int32 `json:"offset"`
}

type ListLeadsRequest struct {
	ProductID string `json:"product_id"`
	Limit     int32  `json:"limit"`
	Offset    int32  `json:"offset"`
}

type ListProductsResponse struct {
	Products []*ProductResponse `json:"products"`
	Total    int32              `json:"total"`
}

type ListLeadsResponse struct {
	Leads []*LeadResponse `json:"leads"`
	Total int32           `json:"total"`
}

type EmptyResponse struct{}

// MongoDB Client
var mongoClient *mongo.Client

// Database and Collections
const (
	DatabaseName       = "grpc_crud_db"
	ProductsCollection = "products"
	LeadsCollection    = "leads"
	MongoURI           = "mongodb://localhost:27017"
)

// Service Implementation
type ProductServiceServer struct {
	productCollection *mongo.Collection
	leadCollection    *mongo.Collection
}

// Schema validation
func validateDataAgainstSchema(data map[string]interface{}, schema map[string]interface{}) error {
	for field, fieldSchema := range schema {
		fieldInfo, ok := fieldSchema.(map[string]interface{})
		if !ok {
			continue
		}

		required, _ := fieldInfo["required"].(bool)
		fieldType, _ := fieldInfo["type"].(string)

		value, exists := data[field]

		// Check if required field is missing
		if required && !exists {
			return fmt.Errorf("required field '%s' is missing", field)
		}

		if !exists {
			continue
		}

		// Validate field type
		if err := validateFieldType(field, value, fieldType); err != nil {
			return err
		}

		// If the field is an object and a nested schema is provided, validate recursively
		if fieldType == "object" {
			var nestedSchema map[string]interface{}
			if ns, ok := fieldInfo["properties"].(map[string]interface{}); ok {
				nestedSchema = ns
			} else if ns, ok := fieldInfo["schema"].(map[string]interface{}); ok {
				nestedSchema = ns
			}

			if nestedSchema != nil {
				// Accept map[string]interface{} (JSON) or bson.M (Mongo)
				var nestedData map[string]interface{}
				if objMap, ok := value.(map[string]interface{}); ok {
					nestedData = objMap
				} else if bm, ok := value.(bson.M); ok {
					nestedData = map[string]interface{}(bm)
				} else {
					return fmt.Errorf("field '%s' must be an object for nested validation", field)
				}

				if err := validateDataAgainstSchema(nestedData, nestedSchema); err != nil {
					return fmt.Errorf("object field '%s' validation failed: %v", field, err)
				}
			}
		}
	}

	return nil
}

func validateFieldType(fieldName string, value interface{}, expectedType string) error {
	// Handle explicit nulls early
	if value == nil {
		if expectedType == "null" {
			return nil
		}
		return fmt.Errorf("field '%s' must not be null", fieldName)
	}

	switch expectedType {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("field '%s' must be a string", fieldName)
		}
	case "number":
		switch value.(type) {
		case int, int32, int64, float32, float64:
			// ok
		default:
			return fmt.Errorf("field '%s' must be a number", fieldName)
		}
	case "double":
		// JSON numbers decode to float64; also accept float32
		switch value.(type) {
		case float32, float64:
			// ok
		default:
			return fmt.Errorf("field '%s' must be a double (floating-point)", fieldName)
		}
	case "boolean", "bool":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("field '%s' must be a boolean", fieldName)
		}
	case "array":
		if reflect.TypeOf(value).Kind() != reflect.Slice {
			return fmt.Errorf("field '%s' must be an array", fieldName)
		}
	case "object":
		if _, ok := value.(map[string]interface{}); !ok {
			return fmt.Errorf("field '%s' must be an object", fieldName)
		}
	case "null":
		// Already handled by early check; reaching here means non-nil value
		return fmt.Errorf("field '%s' must be null", fieldName)
	case "date":
		// Accept time.Time, primitive.DateTime, or ISO/RFC3339 strings
		switch v := value.(type) {
		case time.Time:
			// ok
		case primitive.DateTime:
			// ok
		case string:
			if !isValidISODateString(v) {
				return fmt.Errorf("field '%s' must be a valid ISO date string (e.g., RFC3339)", fieldName)
			}
		default:
			return fmt.Errorf("field '%s' must be a date (time.Time, primitive.DateTime, or ISO string)", fieldName)
		}
	case "timestamp":
		// Accept primitive.Timestamp, integer-like numbers, or numeric strings
		switch v := value.(type) {
		case primitive.Timestamp:
			// ok
		case int, int32, int64:
			// ok
		case float64, float32:
			// ok (JSON numbers)
		case string:
			if _, err := strconv.ParseInt(v, 10, 64); err != nil {
				return fmt.Errorf("field '%s' must be a numeric string representing a timestamp", fieldName)
			}
		default:
			return fmt.Errorf("field '%s' must be a timestamp (integer, numeric string, or primitive.Timestamp)", fieldName)
		}
	}
	return nil
}

// isValidISODateString validates common ISO-8601/RFC3339 date-time formats
func isValidISODateString(s string) bool {
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z07:00",
	}
	for _, layout := range layouts {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	return false
}

// Product CRUD Operations
func (s *ProductServiceServer) CreateProduct(ctx context.Context, req *CreateProductRequest) (*ProductResponse, error) {
	product := &Product{
		ID:          primitive.NewObjectID().Hex(),
		Name:        req.Name,
		Description: req.Description,
		Schema:      req.Schema,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	_, err := s.productCollection.InsertOne(ctx, product)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create product: %v", err)
	}

	return &ProductResponse{
		ID:          product.ID,
		Name:        product.Name,
		Description: product.Description,
		Schema:      product.Schema,
		CreatedAt:   product.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   product.UpdatedAt.Format(time.RFC3339),
	}, nil
}

func (s *ProductServiceServer) GetProduct(ctx context.Context, req *GetProductRequest) (*ProductResponse, error) {
	var product Product
	err := s.productCollection.FindOne(ctx, bson.M{"_id": req.ID}).Decode(&product)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, status.Errorf(codes.NotFound, "product not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to get product: %v", err)
	}

	return &ProductResponse{
		ID:          product.ID,
		Name:        product.Name,
		Description: product.Description,
		Schema:      product.Schema,
		CreatedAt:   product.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   product.UpdatedAt.Format(time.RFC3339),
	}, nil
}

func (s *ProductServiceServer) UpdateProduct(ctx context.Context, req *UpdateProductRequest) (*ProductResponse, error) {
	update := bson.M{
		"$set": bson.M{
			"name":        req.Name,
			"description": req.Description,
			"schema":      req.Schema,
			"updated_at":  time.Now(),
		},
	}

	result, err := s.productCollection.UpdateOne(ctx, bson.M{"_id": req.ID}, update)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update product: %v", err)
	}

	if result.MatchedCount == 0 {
		return nil, status.Errorf(codes.NotFound, "product not found")
	}

	// Return updated product
	return s.GetProduct(ctx, &GetProductRequest{ID: req.ID})
}

func (s *ProductServiceServer) DeleteProduct(ctx context.Context, req *DeleteProductRequest) (*EmptyResponse, error) {
	result, err := s.productCollection.DeleteOne(ctx, bson.M{"_id": req.ID})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete product: %v", err)
	}

	if result.DeletedCount == 0 {
		return nil, status.Errorf(codes.NotFound, "product not found")
	}

	return &EmptyResponse{}, nil
}

func (s *ProductServiceServer) ListProducts(ctx context.Context, req *ListProductsRequest) (*ListProductsResponse, error) {
	limit := int64(req.Limit)
	offset := int64(req.Offset)

	if limit == 0 {
		limit = 10
	}

	opts := options.Find().SetLimit(limit).SetSkip(offset)
	cursor, err := s.productCollection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list products: %v", err)
	}
	defer cursor.Close(ctx)

	var products []*ProductResponse
	for cursor.Next(ctx) {
		var product Product
		if err := cursor.Decode(&product); err != nil {
			continue
		}

		products = append(products, &ProductResponse{
			ID:          product.ID,
			Name:        product.Name,
			Description: product.Description,
			Schema:      product.Schema,
			CreatedAt:   product.CreatedAt.Format(time.RFC3339),
			UpdatedAt:   product.UpdatedAt.Format(time.RFC3339),
		})
	}

	// Get total count
	total, _ := s.productCollection.CountDocuments(ctx, bson.M{})

	return &ListProductsResponse{
		Products: products,
		Total:    int32(total),
	}, nil
}

// Lead CRUD Operations
func (s *ProductServiceServer) CreateLead(ctx context.Context, req *CreateLeadRequest) (*LeadResponse, error) {
	// First, get the product to validate schema
	var product Product
	err := s.productCollection.FindOne(ctx, bson.M{"_id": req.ProductID}).Decode(&product)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, status.Errorf(codes.NotFound, "product not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to get product: %v", err)
	}

	// Validate data against product schema
	if err := validateDataAgainstSchema(req.Data, product.Schema); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "data validation failed: %v", err)
	}

	lead := &Lead{
		ID:        primitive.NewObjectID().Hex(),
		ProductID: req.ProductID,
		Data:      req.Data,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_, err = s.leadCollection.InsertOne(ctx, lead)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create lead: %v", err)
	}

	return &LeadResponse{
		ID:        lead.ID,
		ProductID: lead.ProductID,
		Data:      lead.Data,
		CreatedAt: lead.CreatedAt.Format(time.RFC3339),
		UpdatedAt: lead.UpdatedAt.Format(time.RFC3339),
	}, nil
}

func (s *ProductServiceServer) GetLead(ctx context.Context, req *GetLeadRequest) (*LeadResponse, error) {
	var lead Lead
	err := s.leadCollection.FindOne(ctx, bson.M{"_id": req.ID}).Decode(&lead)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, status.Errorf(codes.NotFound, "lead not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to get lead: %v", err)
	}

	return &LeadResponse{
		ID:        lead.ID,
		ProductID: lead.ProductID,
		Data:      lead.Data,
		CreatedAt: lead.CreatedAt.Format(time.RFC3339),
		UpdatedAt: lead.UpdatedAt.Format(time.RFC3339),
	}, nil
}

func (s *ProductServiceServer) UpdateLead(ctx context.Context, req *UpdateLeadRequest) (*LeadResponse, error) {
	// Get existing lead to get product ID
	var existingLead Lead
	err := s.leadCollection.FindOne(ctx, bson.M{"_id": req.ID}).Decode(&existingLead)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, status.Errorf(codes.NotFound, "lead not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to get lead: %v", err)
	}

	// Get product schema for validation
	var product Product
	err = s.productCollection.FindOne(ctx, bson.M{"_id": existingLead.ProductID}).Decode(&product)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get product for validation: %v", err)
	}

	// Validate data against product schema
	if err := validateDataAgainstSchema(req.Data, product.Schema); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "data validation failed: %v", err)
	}

	update := bson.M{
		"$set": bson.M{
			"data":       req.Data,
			"updated_at": time.Now(),
		},
	}

	result, err := s.leadCollection.UpdateOne(ctx, bson.M{"_id": req.ID}, update)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update lead: %v", err)
	}

	if result.MatchedCount == 0 {
		return nil, status.Errorf(codes.NotFound, "lead not found")
	}

	// Return updated lead
	return s.GetLead(ctx, &GetLeadRequest{ID: req.ID})
}

func (s *ProductServiceServer) DeleteLead(ctx context.Context, req *DeleteLeadRequest) (*EmptyResponse, error) {
	result, err := s.leadCollection.DeleteOne(ctx, bson.M{"_id": req.ID})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete lead: %v", err)
	}

	if result.DeletedCount == 0 {
		return nil, status.Errorf(codes.NotFound, "lead not found")
	}

	return &EmptyResponse{}, nil
}

func (s *ProductServiceServer) ListLeads(ctx context.Context, req *ListLeadsRequest) (*ListLeadsResponse, error) {
	filter := bson.M{}
	if req.ProductID != "" {
		filter["product_id"] = req.ProductID
	}

	limit := int64(req.Limit)
	offset := int64(req.Offset)

	if limit == 0 {
		limit = 10
	}

	opts := options.Find().SetLimit(limit).SetSkip(offset)
	cursor, err := s.leadCollection.Find(ctx, filter, opts)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list leads: %v", err)
	}
	defer cursor.Close(ctx)

	var leads []*LeadResponse
	for cursor.Next(ctx) {
		var lead Lead
		if err := cursor.Decode(&lead); err != nil {
			continue
		}

		leads = append(leads, &LeadResponse{
			ID:        lead.ID,
			ProductID: lead.ProductID,
			Data:      lead.Data,
			CreatedAt: lead.CreatedAt.Format(time.RFC3339),
			UpdatedAt: lead.UpdatedAt.Format(time.RFC3339),
		})
	}

	// Get total count
	total, _ := s.leadCollection.CountDocuments(ctx, filter)

	return &ListLeadsResponse{
		Leads: leads,
		Total: int32(total),
	}, nil
}

// HTTP Handlers for Postman Testing
func (s *ProductServiceServer) setupHTTPHandlers() *mux.Router {
	router := mux.NewRouter()

	// Product routes
	router.HandleFunc("/api/products", s.httpCreateProduct).Methods("POST")
	router.HandleFunc("/api/products/{id}", s.httpGetProduct).Methods("GET")
	router.HandleFunc("/api/products/{id}", s.httpUpdateProduct).Methods("PUT")
	router.HandleFunc("/api/products/{id}", s.httpDeleteProduct).Methods("DELETE")
	router.HandleFunc("/api/products", s.httpListProducts).Methods("GET")

	// Lead routes
	router.HandleFunc("/api/leads", s.httpCreateLead).Methods("POST")
	router.HandleFunc("/api/leads/{id}", s.httpGetLead).Methods("GET")
	router.HandleFunc("/api/leads/{id}", s.httpUpdateLead).Methods("PUT")
	router.HandleFunc("/api/leads/{id}", s.httpDeleteLead).Methods("DELETE")
	router.HandleFunc("/api/leads", s.httpListLeads).Methods("GET")

	return router
}

// HTTP Product Handlers
func (s *ProductServiceServer) httpCreateProduct(w http.ResponseWriter, r *http.Request) {
	var req CreateProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	product, err := s.CreateProduct(r.Context(), &req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(product)
}

func (s *ProductServiceServer) httpGetProduct(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	product, err := s.GetProduct(r.Context(), &GetProductRequest{ID: id})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			http.Error(w, "Product not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(product)
}

func (s *ProductServiceServer) httpUpdateProduct(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req UpdateProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	req.ID = id

	product, err := s.UpdateProduct(r.Context(), &req)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			http.Error(w, "Product not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(product)
}

func (s *ProductServiceServer) httpDeleteProduct(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	_, err := s.DeleteProduct(r.Context(), &DeleteProductRequest{ID: id})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			http.Error(w, "Product not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *ProductServiceServer) httpListProducts(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := int32(10)
	offset := int32(0)

	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = int32(l)
		}
	}
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			offset = int32(o)
		}
	}

	products, err := s.ListProducts(r.Context(), &ListProductsRequest{Limit: limit, Offset: offset})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(products)
}

// HTTP Lead Handlers
func (s *ProductServiceServer) httpCreateLead(w http.ResponseWriter, r *http.Request) {
	var req CreateLeadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	lead, err := s.CreateLead(r.Context(), &req)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			http.Error(w, "Product not found", http.StatusNotFound)
		} else if status.Code(err) == codes.InvalidArgument {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(lead)
}

func (s *ProductServiceServer) httpGetLead(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	lead, err := s.GetLead(r.Context(), &GetLeadRequest{ID: id})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			http.Error(w, "Lead not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(lead)
}

func (s *ProductServiceServer) httpUpdateLead(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req UpdateLeadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	req.ID = id

	lead, err := s.UpdateLead(r.Context(), &req)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			http.Error(w, "Lead not found", http.StatusNotFound)
		} else if status.Code(err) == codes.InvalidArgument {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(lead)
}

func (s *ProductServiceServer) httpDeleteLead(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	_, err := s.DeleteLead(r.Context(), &DeleteLeadRequest{ID: id})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			http.Error(w, "Lead not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *ProductServiceServer) httpListLeads(w http.ResponseWriter, r *http.Request) {
	productID := r.URL.Query().Get("product_id")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := int32(10)
	offset := int32(0)

	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = int32(l)
		}
	}
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			offset = int32(o)
		}
	}

	leads, err := s.ListLeads(r.Context(), &ListLeadsRequest{ProductID: productID, Limit: limit, Offset: offset})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(leads)
}

func initMongoDB() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(MongoURI))
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %v", err)
	}

	// Test the connection
	err = client.Ping(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to ping MongoDB: %v", err)
	}

	mongoClient = client
	log.Println("Connected to MongoDB successfully")
	return nil
}

func main() {
	// Initialize MongoDB
	if err := initMongoDB(); err != nil {
		log.Fatalf("Failed to initialize MongoDB: %v", err)
	}
	defer mongoClient.Disconnect(context.Background())

	// Get database and collections
	db := mongoClient.Database(DatabaseName)
	productCollection := db.Collection(ProductsCollection)
	leadCollection := db.Collection(LeadsCollection)

	// Create service
	service := &ProductServiceServer{
		productCollection: productCollection,
		leadCollection:    leadCollection,
	}

	// Start HTTP server for Postman testing
	httpRouter := service.setupHTTPHandlers()
	go func() {
		log.Printf("HTTP server starting on :8080 for Postman testing")
		log.Fatal(http.ListenAndServe(":8080", httpRouter))
	}()

	// Start gRPC server
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()

	// Register service (this would normally be done with generated proto code)
	// For demonstration, we'll create a simple server setup

	log.Printf("gRPC server starting on :50051")
	log.Printf("HTTP server running on :8080")
	log.Printf("MongoDB connected to: %s", MongoURI)
	log.Printf("Database: %s", DatabaseName)
	log.Printf("Collections: %s, %s", ProductsCollection, LeadsCollection)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
