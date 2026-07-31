# Batch Jobs

Submit batch jobs for parallel processing across multiple LLM backends.

## Overview

The batch jobs endpoints allow you to submit multiple chat completion requests for parallel processing. Jobs are processed across all healthy backends simultaneously, with results streaming back as they complete. This enables high-throughput processing without waiting for all requests to finish.

## Endpoints

### Create Job
```http
POST /v1/jobs
Content-Type: application/json
```

Submit a batch of requests for parallel processing.

### Get Job Status
```http
GET /v1/jobs/{id}
```

Retrieve the current status and results of a job.

### Cancel Job
```http
POST /v1/jobs/{id}/cancel
```

Cancel a running job (pending or processing jobs only).

## Request Body Schema

### CreateJobRequest

| Field | Type | Required | Description | Constraints |
|-------|------|----------|-------------|-------------|
| `system_prompt` | string | No | System prompt applied to all requests in the batch | Optional; extracted from requests if not provided |
| `requests` | array[JobRequest] | **Yes** | Array of individual chat requests | Minimum 1 request |
| `model` | string | No | Model identifier hint | Any string; each backend uses its configured model |

### JobRequest Object

| Field | Type | Required | Description | Constraints |
|-------|------|----------|-------------|-------------|
| `id` | string | **Yes** | Unique identifier for this request within the job | Must be unique within the batch |
| `messages` | array[Message] | **Yes** | Array of conversation messages | Minimum 1 message |

### Message Object

| Field | Type | Required | Description | Allowed Values |
|-------|------|----------|-------------|----------------|
| `role` | string | **Yes** | Role of the message author | `system`, `user`, `assistant` |
| `content` | string | **Yes** | Content of the message | Any non-empty string |

## Examples

### Create a Batch Job

```bash
curl -X POST http://localhost:8080/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "system_prompt": "You are a helpful assistant. Be concise.",
    "requests": [
      {
        "id": "request-1",
        "messages": [
          {"role": "user", "content": "What is the capital of France?"}
        ]
      },
      {
        "id": "request-2",
        "messages": [
          {"role": "user", "content": "Who wrote Romeo and Juliet?"}
        ]
      },
      {
        "id": "request-3",
        "messages": [
          {"role": "user", "content": "What is 2 + 2?"}
        ]
      }
    ]
  }'
```

### Get Job Status

```bash
curl http://localhost:8080/v1/jobs/job-abc123def456
```

### Cancel a Job

```bash
curl -X POST http://localhost:8080/v1/jobs/job-abc123def456/cancel
```

## Response

### Create Job Response (HTTP 200)

#### Response Body

```json
{
  "job_id": "job-abc123def456",
  "status": "pending",
  "total": 3,
  "completed": 0,
  "failed": 0,
  "created_at": 1704067200,
  "updated_at": 1704067200
}
```

#### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `job_id` | string | Unique job identifier prefixed with `job-` |
| `status` | string | Current job status: `pending`, `processing`, `completed`, `failed`, `partial_failure`, `cancelled` |
| `total` | integer | Total number of requests in the job |
| `completed` | integer | Number of requests successfully completed |
| `failed` | integer | Number of requests that failed |
| `created_at` | integer | Unix timestamp when job was created |
| `updated_at` | integer | Unix timestamp when job was last updated |

### Get Job Status Response (HTTP 200)

#### Response Body

```json
{
  "job_id": "job-abc123def456",
  "status": "processing",
  "total": 3,
  "completed": 1,
  "failed": 0,
  "results": [
    {
      "id": "request-1",
      "status": "completed",
      "content": "The capital of France is Paris.",
      "backend": "openai",
      "model": "gpt-4o",
      "duration_ms": 1234
    }
  ],
  "created_at": 1704067200,
  "updated_at": 1704067201
}
```

#### Complete Job Response (Completed Status)

```json
{
  "job_id": "job-abc123def456",
  "status": "completed",
  "total": 3,
  "completed": 3,
  "failed": 0,
  "results": [
    {
      "id": "request-1",
      "status": "completed",
      "content": "The capital of France is Paris.",
      "backend": "openai",
      "model": "gpt-4o",
      "duration_ms": 1234
    },
    {
      "id": "request-2",
      "status": "completed",
      "content": "Romeo and Juliet was written by William Shakespeare.",
      "backend": "openrouter",
      "model": "anthropic/claude-3-5-sonnet",
      "duration_ms": 1567
    },
    {
      "id": "request-3",
      "status": "completed",
      "content": "2 + 2 equals 4.",
      "backend": "openai",
      "model": "gpt-4o",
      "duration_ms": 987
    }
  ],
  "created_at": 1704067200,
  "updated_at": 1704067203
}
```

#### Partial Failure Response

```json
{
  "job_id": "job-abc123def456",
  "status": "partial_failure",
  "total": 3,
  "completed": 2,
  "failed": 1,
  "results": [
    {
      "id": "request-1",
      "status": "completed",
      "content": "The capital of France is Paris.",
      "backend": "openai",
      "model": "gpt-4o",
      "duration_ms": 1234
    },
    {
      "id": "request-2",
      "status": "failed",
      "error": "Backend quota exceeded",
      "backend": "openrouter",
      "model": "anthropic/claude-3-5-sonnet",
      "duration_ms": 1567
    },
    {
      "id": "request-3",
      "status": "completed",
      "content": "2 + 2 equals 4.",
      "backend": "openai",
      "model": "gpt-4o",
      "duration_ms": 987
    }
  ],
  "created_at": 1704067200,
  "updated_at": 1704067203
}
```

#### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `job_id` | string | Unique job identifier |
| `status` | string | Current job status: `pending`, `processing`, `completed`, `failed`, `partial_failure`, `cancelled` |
| `total` | integer | Total number of requests in the job |
| `completed` | integer | Number of requests successfully completed |
| `failed` | integer | Number of requests that failed |
| `results` | array[JobResult] | Array of individual request results (only present for processing/completed jobs) |
| `results[].id` | string | Request ID from the original batch |
| `results[].status` | string | Individual request status: `completed`, `failed` |
| `results[].content` | string | Generated response content (present when status is `completed`) |
| `results[].error` | string | Error message (present when status is `failed`) |
| `results[].backend` | string | Backend that processed this request |
| `results[].model` | string | Model used for this request |
| `results[].duration_ms` | integer | Processing duration in milliseconds |
| `created_at` | integer | Unix timestamp when job was created |
| `updated_at` | integer | Unix timestamp when job was last updated |

### Cancel Job Response

#### Success (HTTP 200)
```json
{
  "success": true,
  "message": "Job cancelled successfully"
}
```

#### Failure - Job Not Found (HTTP 404)
```json
{
  "error": {
    "message": "Job not found",
    "type": "invalid_request",
    "code": "job_not_found"
  }
}
```

#### Failure - Job Already Terminal (HTTP 400)
```json
{
  "error": {
    "message": "Job cannot be cancelled because it is already completed",
    "type": "invalid_request",
    "code": "job_terminal"
  }
}
```

## Job Status Values

The following status values are used to track job progress:

| Status | Description |
|--------|-------------|
| `pending` | Job has been created but processing hasn't started yet |
| `processing` | Job is actively being processed across backends |
| `completed` | All requests in the job have completed successfully |
| `partial_failure` | Some requests failed, but at least one succeeded |
| `failed` | All requests in the job failed |
| `cancelled` | Job was cancelled by user request |

**Terminal Statuses**: `completed`, `partial_failure`, `failed`, and `cancelled` are terminal statuses. Once a job reaches one of these statuses, no further processing occurs.

**Non-terminal Statuses**: `pending` and `processing` are non-terminal statuses. Jobs in these statuses can be cancelled.

## Behavior Notes

### Parallel Processing
- Requests are distributed across all healthy backends simultaneously
- Each backend processes requests in parallel according to its configured `workers` count
- Results stream back as they complete - no waiting for all requests to finish

### System Prompt Handling
- If `system_prompt` is provided in the request, it's applied to all requests
- If not provided, system prompts are extracted from individual request messages
- Multiple system messages within a request are concatenated

### Backend Distribution
- Requests are distributed in round-robin fashion across healthy backends
- Each backend processes its assigned requests in parallel
- Backend circuit breakers and key rotation apply during job processing

### Job Cleanup
- Completed jobs are automatically cleaned up after 1 hour
- You can poll job status periodically until completion
- For long-term storage, save results before cleanup

### Error Handling
- Individual request failures don't stop the entire job
- The job continues processing other requests
- Final job status reflects overall success/failure rate

## Common Use Cases

### Batch Processing Multiple Questions
```bash
curl -X POST http://localhost:8080/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "system_prompt": "Answer questions factually and concisely.",
    "requests": [
      {
        "id": "q1",
        "messages": [
          {"role": "user", "content": "What is the population of Tokyo?"}
        ]
      },
      {
        "id": "q2",
        "messages": [
          {"role": "user", "content": "Who invented the telephone?"}
        ]
      },
      {
        "id": "q3",
        "messages": [
          {"role": "user", "content": "What is the chemical symbol for gold?"}
        ]
      }
    ]
  }'
```

### Processing a List of Documents
```bash
curl -X POST http://localhost:8080/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "system_prompt": "Summarize the following text in one sentence.",
    "requests": [
      {
        "id": "doc1",
        "messages": [
          {"role": "user", "content": "The quick brown fox jumps over the lazy dog..."}
        ]
      },
      {
        "id": "doc2",
        "messages": [
          {"role": "user", "content": "Artificial intelligence is transforming..."}
        ]
      }
    ]
  }'
```

### Multi-turn Conversation Batch
```bash
curl -X POST http://localhost:8080/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "requests": [
      {
        "id": "conv1",
        "messages": [
          {"role": "system", "content": "You are a math tutor."},
          {"role": "user", "content": "What is 5 * 5?"},
          {"role": "assistant", "content": "5 * 5 equals 25."},
          {"role": "user", "content": "What about 6 * 6?"}
        ]
      },
      {
        "id": "conv2",
        "messages": [
          {"role": "system", "content": "You are a history expert."},
          {"role": "user", "content": "When was the Declaration of Independence signed?"},
          {"role": "assistant", "content": "July 4, 1776."},
          {"role": "user", "content": "Who was the primary author?"}
        ]
      }
    ]
  }'
```

## Integration Notes

### Polling Strategy
For long-running jobs, poll periodically to check status:

```bash
# Initial submission
JOB_ID=$(curl -X POST http://localhost:8080/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{"requests": [...]}' \
  | jq -r '.job_id')

# Poll every 5 seconds until completion
while true; do
  STATUS=$(curl -s http://localhost:8080/v1/jobs/$JOB_ID | jq -r '.status')
  echo "Job status: $STATUS"

  if [[ "$STATUS" == "completed" || "$STATUS" == "failed" || "$STATUS" == "partial_failure" || "$STATUS" == "cancelled" ]]; then
    echo "Job finished with status: $STATUS"
    break
  fi

  sleep 5
done

# Get final results
curl http://localhost:8080/v1/jobs/$JOB_ID | jq '.results'
```

### Error Recovery
If a job fails partially, you can resubmit only the failed requests:

```bash
# Get failed request IDs
FAILED_IDS=$(curl -s http://localhost:8080/v1/jobs/$JOB_ID \
  | jq -r '.results[] | select(.status == "failed") | .id')

# Create new job with only failed requests (extract original requests from your application)
```

### Rate Limiting Considerations
- Each backend enforces its own rate limits
- Job processing respects backend circuit breakers
- Failed requests due to rate limits are counted in `failed` tally
- Consider staggering large jobs or using multiple API keys per provider