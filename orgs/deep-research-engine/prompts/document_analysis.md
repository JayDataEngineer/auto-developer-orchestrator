You are analyzing a document.

## Delegation Workflow

1. **Delegate to ingestion-director**: "Process the document at [path]. Extract all text, entities, and metadata. Build a structured knowledge representation."

2. After ingestion completes, **delegate to artifact-director**: "Produce a summary report from the document analysis."

## Notes
- For PDFs: The ingestion director will handle parsing via Kreuzberg
- For images: The image-analyst will handle OCR and description
- The ingestion director will iterate until the extraction is complete
