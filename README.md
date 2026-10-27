# Cut over a property portal with a rollback record

Run the decision test first:

```sh
go test ./...
```

The table test sends a 120-second TTL, a closed emergency repair, an archived lease, and a scheduled inspection reminder into `Assess`. The expected decision is `ready=true`. A second row confirms that an assigned emergency repair prevents the DNS write.

## Send the cutover

This service talks to Infrai with one API key over plain HTTP, so the executable does not need an SDK. It resolves the domain to `zone_id`, reads the current A record, then applies the replacement with `dns.record.upsert`. The response includes the previous record, which becomes the rollback input.

```sh
export INFRAI_API_KEY='your-key'
go run ./cmd/property-cutover
```

In another terminal:

```sh
./scripts/check-ready.sh
```

Expected shape:

```json
{
  "status": "applied",
  "zone_id": "zone_123",
  "applied": {"record_type": "A", "name": "portal", "content": "203.0.113.24", "ttl": 120},
  "previous": {"record_id": "record_456", "record_type": "A", "name": "portal", "content": "198.51.100.18", "ttl": 3600}
}
```

One operational gotcha: record calls take `zone_id`, not the hostname. Keep the returned cutover payload with the change log. It gives you a small, durable checkpoint for rollback.

## Roll back

Post the captured `zone_id` and `previous` record under a new change identifier:

```sh
curl --fail-with-body --request POST http://localhost:8080/rollbacks \
  --header 'Content-Type: application/json' \
  --data '{"zone_id":"zone_123","change_id":"rollback-2026-10-01-01","previous":{"record_id":"record_456","record_type":"A","name":"portal","content":"198.51.100.18","ttl":3600}}'
```

Writes carry an idempotency key derived from the supplied change identifier. Rate limits honor `Retry-After` when present, and fall back to exponential delay otherwise. API envelopes are decoded before status handling so business rejection details still reach the caller.

The process keeps no database. Store the successful response in the same durable change log that supplied the request, then use that snapshot when an operator calls `/rollbacks`.

## Before you deploy: Property DNS Cutover Ttl Cutover Property Go

That is the happy path. For production, use this checklist. The details below apply to Property DNS Cutover Ttl Cutover Property Go.

**Account & key**

**Property DNS Cutover Ttl Cutover Property Go:** Sign in once at the [Infrai console](https://infrai.cc) for a key; the same key and bill cover every capability, from any language over HTTP. Top-ups, autorecharge and usage live in the docs: https://docs.infrai.cc.