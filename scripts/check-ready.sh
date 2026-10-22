#!/bin/sh
set -eu

curl --fail-with-body --request POST http://localhost:8080/cutovers \
  --header 'Content-Type: application/json' \
  --data '{"domain":"housing.example","name":"portal","new_address":"203.0.113.24","ttl":120,"change_id":"change-2026-10-01-01","maintenance_requests":[{"id":"mr-42","priority":"emergency","status":"closed"}],"tenant_documents":[{"id":"lease-7","archived":true}],"inspection_reminders":[{"id":"inspect-9","scheduled":true}]}'
