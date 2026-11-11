-- 084_laser_calibrations.sql: a laser's camera, fitted to its bed (#244,
-- docs/printing.md#camera-alignment).
--
-- laser_calibrations hold, for each laser with a camera, the latest fit of
-- its photos' pixels to millimeters on its bed, made from calibration
-- marks it engraved, and good only at the height it was made at and for
-- the model it was made for.
--
--   - homography maps a pixel of a photo image_width × image_height to the
--     bed, as JSON: nine numbers, row by row; a photo of another size of
--     the same view is scaled to it;
--   - z_mm is how high the scrap's surface stood above the bed: a job at
--     another height asks for calibrating again;
--   - marks is how many marks were found and fitted; rms_mm and max_mm are
--     how far they landed from where they were engraved;
--   - calibrated_by is a user's or an API token's ID, and calibrated_by_token
--     says which.

CREATE TABLE laser_calibrations (
    printer_id VARCHAR(36) PRIMARY KEY,
    model VARCHAR(64) NOT NULL,
    homography TEXT NOT NULL,
    image_width INTEGER NOT NULL,
    image_height INTEGER NOT NULL,
    z_mm DOUBLE PRECISION NOT NULL,
    marks INTEGER NOT NULL,
    rms_mm DOUBLE PRECISION NOT NULL,
    max_mm DOUBLE PRECISION NOT NULL,
    calibrated_by VARCHAR(36),
    calibrated_by_token BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_laser_calibrations_printer FOREIGN KEY (printer_id) REFERENCES printers(id) ON DELETE CASCADE
);
