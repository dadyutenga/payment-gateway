-- Migration: 000056_creator_payout_destinations.down.sql

DROP TRIGGER IF EXISTS update_creator_payout_destinations_updated_at ON app.creator_payout_destinations;
DROP TABLE IF EXISTS app.creator_payout_destinations;
