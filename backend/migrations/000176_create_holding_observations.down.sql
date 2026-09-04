-- Reverses 000176. The table records only that a capture ran; dropping it loses no user data,
-- and the portfolio re-link rule degrades back to treating a confirmed-empty successor as an
-- unobserved one (it retains the predecessor, which is the safe direction).
DROP TABLE IF EXISTS holding_observations;
