-- Migration: 000053_creator_survey.down.sql

DROP TRIGGER IF EXISTS update_creator_onboarding_surveys_updated_at ON app.creator_onboarding_surveys;
DROP TABLE IF EXISTS app.creator_onboarding_surveys;
