# Postman API Testing Guide

This repository exposes both an HTTP API and a gRPC server backed by MongoDB for managing Products and Leads with schema-based validation.

## Prerequisites

- Go (recommended: 1.20+)
- MongoDB running locally on `localhost:27017`
- (Optional) Postman for testing the HTTP API

## Setup

```bash
go mod tidy
```

## Run

```bash
go run main.go
```

### Servers

- gRPC Server: `localhost:50051`
- HTTP API Server: `http://localhost:8080`

---

## Schema Validation Reference

The HTTP API validates each lead object's `data` against its product `schema`.

- Types: `string`, `number`, `double`, `boolean` (or `bool`), `array`, `object`, `null`, `date`, `timestamp`
- Common keys: `type` (string, required), `required` (boolean, optional)

Additional constraints by type:

- string: `pattern` (regex), `minLength` (int), `maxLength` (int)
- number/double: `minimum` (number), `maximum` (number)
- object: nested schema via `properties` or `schema`
- array: MUST define `items` as either a type string (e.g., `"string"`) or a nested schema object; each element is validated

Global rules and notes:

- Missing required fields return: `required field '<name>' is missing`
- Extra/unknown fields in `data` are NOT allowed and return: `unknown field '<name>' is not allowed`
- `null` is only accepted when `type` is `null`
- `date` accepts ISO/RFC3339 strings, or native date types server-side
- `timestamp` accepts integers, floats, or numeric strings (e.g., `1691582400` or "1691582400")
- Schema definition is also validated (allowed keys by type, field names cannot start with `$` or contain `.`, arrays must define `items`)

### Examples

String constraints:

```json
{
  "username": { "type": "string", "required": true, "pattern": "^[a-zA-Z0-9_]+$", "minLength": 3, "maxLength": 20 }
}
```

Number constraints:

```json
{
  "age": { "type": "number", "required": false, "minimum": 0, "maximum": 120 }
}
```

Nested object (use `properties` or `schema` for nested validation):

```json
{
  "user_info": {
    "type": "object",
    "required": true,
    "properties": {
      "first_name": { "type": "string", "required": true },
      "last_name": { "type": "string", "required": true },
      "age": { "type": "number", "required": false, "minimum": 0 }
    }
  }
}
```

Array with typed items:

```json
{
  "interests": { "type": "array", "required": false, "items": "string" }
}
```

Date and timestamp:

```json
{
  "signup_date": { "type": "date", "required": true },
  "last_seen_ts": { "type": "timestamp", "required": false }
}
```

---

## Postman Requests

### 1. Create Product

- **Method:** `POST`
- **URL:** `http://localhost:8080/api/products`
- **Headers:**

```http
Content-Type: application/json
```

- **Body:**

```json
{
  "name": "Email Marketing Product",
  "description": "A product for collecting email marketing leads",
  "schema": {
    "name": { "type": "string", "required": true },
    "email": { "type": "string", "required": true },
    "age": { "type": "number", "required": false, "minimum": 0 },
    "phone": { "type": "string", "required": false },
    "interests": { "type": "array", "required": false, "items": "string" }
  }
}
```

- **Expected Response:**

```json
{
  "id": "64f8b1a2e5c6d7f8a9b0c1d2",
  "name": "Email Marketing Product",
  "description": "A product for collecting email marketing leads",
  "schema": {
    "name": { "type": "string", "required": true },
    "email": { "type": "string", "required": true },
    "age": { "type": "number", "required": false, "minimum": 0 },
    "phone": { "type": "string", "required": false },
    "interests": { "type": "array", "required": false, "items": "string" }
  },
  "created_at": "2024-08-09T12:00:00Z",
  "updated_at": "2024-08-09T12:00:00Z"
}
```

---

### 2. Get Product by ID

- **Method:** `GET`
- **URL:** `http://localhost:8080/api/products/{product_id}`

Example: `http://localhost:8080/api/products/64f8b1a2e5c6d7f8a9b0c1d2`

---

### 3. List All Products

- **Method:** `GET`
- **URL:** `http://localhost:8080/api/products`
- **Query Parameters (optional):**
  - `limit`: number of products to return (default: 10)
  - `offset`: number of products to skip (default: 0)

Example: `http://localhost:8080/api/products?limit=5&offset=0`

---

### 4. Update Product

- **Method:** `PUT`
- **URL:** `http://localhost:8080/api/products/{product_id}`
- **Headers:**

```http
Content-Type: application/json
```

- **Body:**

```json
{
  "name": "Updated Email Marketing Product",
  "description": "Updated description for email marketing leads",
  "schema": {
    "name": { "type": "string", "required": true },
    "email": { "type": "string", "required": true },
    "age": { "type": "number", "required": false },
    "company": { "type": "string", "required": true }
  }
}
```

---

### 5. Delete Product

- **Method:** `DELETE`
- **URL:** `http://localhost:8080/api/products/{product_id}`
- **Expected Response:** `204 No Content`

---

### 6. Create Valid Lead

- **Method:** `POST`
- **URL:** `http://localhost:8080/api/leads`
- **Headers:**

```http
Content-Type: application/json
```

- **Body:**

```json
{
  "phone_number": "+1234567890",
  "product_id": "64f8b1a2e5c6d7f8a9b0c1d2",
  "data": {
    "name": "John Doe",
    "email": "john.doe@example.com",
    "age": 30,
    "phone": "+1234567890",
    "interests": ["technology", "marketing"]
  }
}
```

- **Behavior:**
  - If a lead with the same `phone_number` exists, the new `{ product_id, data }` is appended to its `objects` array.
  - Otherwise, a new lead is created.

- **Expected Response:**

```json
{
  "id": "64f8b1a2e5c6d7f8a9b0c1d3",
  "phone_number": "+1234567890",
  "objects": [
    {
      "product_id": "64f8b1a2e5c6d7f8a9b0c1d2",
      "data": {
        "name": "John Doe",
        "email": "john.doe@example.com",
        "age": 30,
        "phone": "+1234567890",
        "interests": ["technology", "marketing"]
      }
    }
  ],
  "created_at": "2024-08-09T12:05:00Z",
  "updated_at": "2024-08-09T12:05:00Z"
}
```

---

### 7. Create Invalid Lead (Missing Required Field)

- **Method:** `POST`
- **URL:** `http://localhost:8080/api/leads`
- **Headers:**

```http
Content-Type: application/json
```

- **Body (missing required `email`):**

```json
{
  "phone_number": "+1234567899",
  "product_id": "64f8b1a2e5c6d7f8a9b0c1d2",
  "data": {
    "name": "Jane Smith",
    "age": 25
  }
}
```

- **Expected Response:** `400 Bad Request`

```json
{
  "error": "data validation failed: required field 'email' is missing"
}
```

---

### 8. Create Invalid Lead (Wrong Data Type)

- **Method:** `POST`
- **URL:** `http://localhost:8080/api/leads`
- **Headers:**

```http
Content-Type: application/json
```

- **Body (age should be a number):**

```json
{
  "phone_number": "+1234567898",
  "product_id": "64f8b1a2e5c6d7f8a9b0c1d2",
  "data": {
    "name": "Bob Wilson",
    "email": "bob@example.com",
    "age": "thirty"
  }
}
```

- **Expected Response:** `400 Bad Request`

```json
{
  "error": "data validation failed: field 'age' must be a number"
}
```

- **Unknown field example (assuming schema has no `nickname`):**

```json
{
  "phone_number": "+1234567897",
  "product_id": "64f8b1a2e5c6d7f8a9b0c1d2",
  "data": {
    "name": "Alice",
    "email": "alice@example.com",
    "nickname": "ally"
  }
}
```

- **Expected Response:** `400 Bad Request`

```json
{
  "error": "data validation failed: unknown field 'nickname' is not allowed"
}
```

---

### 9. Get Lead by ID

- **Method:** `GET`
- **URL:** `http://localhost:8080/api/leads/{lead_id}`

Replace `{lead_id}` with the actual ID from the create response.

---

### 10. List Leads

- **Method:** `GET`
- **URL:** `http://localhost:8080/api/leads`
- **Query Parameters (optional):**
  - `product_id`: filter leads that have at least one object with this product ID
  - `limit`: number of leads to return (default: 10)
  - `offset`: number of leads to skip (default: 0)

Examples:

- All leads: `http://localhost:8080/api/leads`
- Leads for specific product: `http://localhost:8080/api/leads?product_id=64f8b1a2e5c6d7f8a9b0c1d2`
- Paginated: `http://localhost:8080/api/leads?limit=5&offset=10`

---

### 11. Update Lead

- **Method:** `PUT`
- **URL:** `http://localhost:8080/api/leads/{lead_id}`
- **Headers:**

```http
Content-Type: application/json
```

- **Body (replace entire `objects` array; each item is validated against its product's schema):**

```json
{
  "objects": [
    {
      "product_id": "64f8b1a2e5c6d7f8a9b0c1d2",
      "data": {
        "name": "John Updated",
        "email": "john.updated@example.com",
        "age": 31,
        "phone": "+1234567891",
        "interests": ["technology", "marketing", "sales"]
      }
    }
  ]
}
```

---

### 12. Delete Lead

- **Method:** `DELETE`
- **URL:** `http://localhost:8080/api/leads/{lead_id}`
- **Expected Response:** `204 No Content`

---

## Testing Workflow

### Step-by-Step

1. Create a product using request #1
2. Copy the product ID from the response
3. Create valid leads using request #6 (include `phone_number` and your `product_id`)
4. Test validation using requests #7 and #8
5. List products and leads to verify data
6. Perform update and delete operations to test full CRUD

### Schema Validation Test Cases

**Test Case 1: Required Field Validation**

- Create a product with required fields
- Try to create a lead missing required `phone_number` or required schema fields
- Should return 400 Bad Request

**Test Case 2: Data Type Validation**

- Create a lead with wrong data types (e.g., string instead of number)
- Should return 400 Bad Request with specific error message

**Test Case 3: Non-existent Product**

- Try to create a lead with invalid `product_id`
- Should return 404 Not Found

**Test Case 4: Complex Schema**

```json
{
  "name": "Complex Product",
  "description": "Product with complex validation rules",
  "schema": {
    "user_info": {
      "type": "object",
      "required": true,
      "properties": {
        "first_name": { "type": "string", "required": true },
        "prefs": {
          "type": "object",
          "required": false,
          "schema": {
            "theme": { "type": "string", "required": false },
            "emails": { "type": "boolean", "required": true }
          }
        }
      }
    },
    "tags": { "type": "array", "required": false, "items": "string" },
    "is_active": { "type": "boolean", "required": true },
    "score": { "type": "number", "required": true, "minimum": 0 }
  }
}
```

---

## gRPC

- Service definitions are in `leads.proto`; generated code is under `pb/`.
- Server listens on `localhost:50051`.


