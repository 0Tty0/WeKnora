-- Roll back migration 000084 processing-artifact persistence and attempt fencing.
DROP TABLE IF EXISTS knowledge_attempt_counters;
DROP TABLE IF EXISTS processing_artifacts;
