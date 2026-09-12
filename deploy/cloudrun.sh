#!/bin/sh
# Deploy tilegen's HTTP service to Cloud Run.
#
#   PROJECT=my-gcp-project deploy/cloudrun.sh
#
# The service holds no state and stores nothing, so it scales to zero and
# needs no database, no bucket and no service account beyond the default.
set -eu

PROJECT="${PROJECT:?set PROJECT to your Google Cloud project id}"
REGION="${REGION:-us-central1}"
SERVICE="${SERVICE:-tilegen}"

gcloud run deploy "$SERVICE" \
  --project "$PROJECT" \
  --region "$REGION" \
  --source . \
  --allow-unauthenticated \
  --cpu 1 \
  --memory 512Mi \
  --concurrency 40 \
  --timeout 60s \
  --max-instances 10 \
  --set-env-vars "GOMAXPROCS=1"

gcloud run services describe "$SERVICE" --project "$PROJECT" --region "$REGION" \
  --format 'value(status.url)'
