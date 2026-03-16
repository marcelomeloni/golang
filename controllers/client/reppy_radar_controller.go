package client

import (
	"database/sql"
	"log"
	"net/http"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type RadarProfile struct {
	UserID    string `json:"userId"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
	Instagram string `json:"instagram,omitempty"`
	TappedByMe bool  `json:"tappedByMe"`
	IsMutual   bool  `json:"isMutual"`
}

type BlockedProfile struct {
	UserID    string `json:"userId"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
}

type ToggleRadarRequest struct {
	Enabled bool `json:"enabled"`
}

func hasValidTicket(db *sql.DB, userID, eventID string) (bool, error) {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM tickets t
		JOIN orders o ON o.id = t.order_id
		WHERE t.user_id  = $1
		  AND o.event_id = $2
		  AND t.status   = 'valid'
		  AND o.status   = 'paid'
	`, userID, eventID).Scan(&count)
	return count > 0, err
}

func ToggleRadarMode(c *gin.Context) {
	userID, _ := c.Get("userID")
	eventID := c.Param("eventId")

	var req ToggleRadarRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "payload inválido"})
		return
	}

	db := config.GetDB()

	eligible, err := hasValidTicket(db, userID.(string), eventID)
	if err != nil {
		log.Printf("ToggleRadarMode eligibility: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}
	if !eligible {
		c.JSON(http.StatusForbidden, gin.H{"error": "ingresso válido não encontrado para este evento"})
		return
	}

	_, err = db.Exec(`
		UPDATE tickets
		SET radar_enabled = $1
		WHERE user_id = $2
		  AND status  = 'valid'
		  AND order_id IN (
		      SELECT id FROM orders
		      WHERE event_id = $3 AND status = 'paid'
		  )
	`, req.Enabled, userID, eventID)
	if err != nil {
		log.Printf("ToggleRadarMode update: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar modo radar"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"radarEnabled": req.Enabled})
}

func GetRadarMode(c *gin.Context) {
	userID, _ := c.Get("userID")
	eventID := c.Param("eventId")

	db := config.GetDB()

	eligible, err := hasValidTicket(db, userID.(string), eventID)
	if err != nil {
		log.Printf("GetRadarMode eligibility: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}
	if !eligible {
		c.JSON(http.StatusForbidden, gin.H{"error": "ingresso válido não encontrado para este evento"})
		return
	}

	var enabled bool
	err = db.QueryRow(`
		SELECT COALESCE(bool_or(t.radar_enabled), false)
		FROM tickets t
		JOIN orders o ON o.id = t.order_id
		WHERE t.user_id  = $1
		  AND o.event_id = $2
		  AND t.status   = 'valid'
		  AND o.status   = 'paid'
	`, userID, eventID).Scan(&enabled)
	if err != nil {
		log.Printf("GetRadarMode query: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"radarEnabled": enabled})
}

func GetRadarProfiles(c *gin.Context) {
	userID, _ := c.Get("userID")
	eventID := c.Param("eventId")

	db := config.GetDB()

	eligible, err := hasValidTicket(db, userID.(string), eventID)
	if err != nil {
		log.Printf("GetRadarProfiles eligibility: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}
	if !eligible {
		c.JSON(http.StatusForbidden, gin.H{"error": "ingresso válido não encontrado para este evento"})
		return
	}

	rows, err := db.Query(`
		SELECT DISTINCT ON (u.id)
			u.id,
			SPLIT_PART(u.full_name, ' ', 1)   AS first_name,
			COALESCE(u.avatar_url, '')         AS avatar_url,
			COALESCE(u.instagram,  '')         AS instagram,
			EXISTS (
				SELECT 1 FROM radar_taps rt
				WHERE rt.event_id     = $2
				  AND rt.from_user_id = $1
				  AND rt.to_user_id   = u.id
			) AS tapped_by_me,
			COALESCE((
				SELECT rt.is_mutual FROM radar_taps rt
				WHERE rt.event_id     = $2
				  AND rt.from_user_id = $1
				  AND rt.to_user_id   = u.id
				LIMIT 1
			), false) AS is_mutual
		FROM tickets t
		JOIN orders o ON o.id  = t.order_id
		JOIN users  u ON u.id  = t.user_id
		WHERE o.event_id       = $2
		  AND o.status         = 'paid'
		  AND t.status         = 'valid'
		  AND t.radar_enabled  = true
		  AND t.user_id       != $1
		  AND NOT EXISTS (
		      SELECT 1 FROM radar_blocks rb
		      WHERE (rb.blocker_user_id = $1 AND rb.blocked_user_id = u.id)
		         OR (rb.blocker_user_id = u.id AND rb.blocked_user_id = $1)
		  )
		ORDER BY u.id, u.created_at DESC
	`, userID, eventID)
	if err != nil {
		log.Printf("GetRadarProfiles query: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}
	defer rows.Close()

	profiles := []RadarProfile{}
	for rows.Next() {
		var p RadarProfile
		if err := rows.Scan(&p.UserID, &p.Name, &p.AvatarURL, &p.Instagram, &p.TappedByMe, &p.IsMutual); err != nil {
			log.Printf("GetRadarProfiles scan: %v", err)
			continue
		}
		profiles = append(profiles, p)
	}

	c.JSON(http.StatusOK, gin.H{"profiles": profiles})
}

func TapUser(c *gin.Context) {
	fromUserID, _ := c.Get("userID")
	eventID := c.Param("eventId")
	toUserID := c.Param("targetUserId")

	if fromUserID.(string) == toUserID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "você não pode dar tap em si mesmo"})
		return
	}

	db := config.GetDB()

	eligible, err := hasValidTicket(db, fromUserID.(string), eventID)
	if err != nil {
		log.Printf("TapUser eligibility: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}
	if !eligible {
		c.JSON(http.StatusForbidden, gin.H{"error": "ingresso válido não encontrado para este evento"})
		return
	}

	var blocked int
	db.QueryRow(`
		SELECT COUNT(*) FROM radar_blocks
		WHERE (blocker_user_id = $1 AND blocked_user_id = $2)
		   OR (blocker_user_id = $2 AND blocked_user_id = $1)
	`, fromUserID, toUserID).Scan(&blocked)
	if blocked > 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "ação não permitida"})
		return
	}

	var already int
	db.QueryRow(`
		SELECT COUNT(*) FROM radar_taps
		WHERE event_id     = $1
		  AND from_user_id = $2
		  AND to_user_id   = $3
	`, eventID, fromUserID, toUserID).Scan(&already)
	if already > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "tap já registrado"})
		return
	}

	var reverseTapID string
	reverseExists := db.QueryRow(`
		SELECT id FROM radar_taps
		WHERE event_id     = $1
		  AND from_user_id = $2
		  AND to_user_id   = $3
	`, eventID, toUserID, fromUserID).Scan(&reverseTapID) == nil

	isMutual := reverseExists

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO radar_taps (id, event_id, from_user_id, to_user_id, is_mutual, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`, uuid.New().String(), eventID, fromUserID, toUserID, isMutual)
	if err != nil {
		log.Printf("TapUser insert: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao registrar tap"})
		return
	}

	if isMutual {
		_, err = tx.Exec(`UPDATE radar_taps SET is_mutual = true WHERE id = $1`, reverseTapID)
		if err != nil {
			log.Printf("TapUser mutual update: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao confirmar match"})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao confirmar tap"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"isMutual": isMutual})
}

func RemoveTap(c *gin.Context) {
	fromUserID, _ := c.Get("userID")
	eventID := c.Param("eventId")
	toUserID := c.Param("targetUserId")

	db := config.GetDB()

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		DELETE FROM radar_taps
		WHERE event_id     = $1
		  AND from_user_id = $2
		  AND to_user_id   = $3
	`, eventID, fromUserID, toUserID)
	if err != nil {
		log.Printf("RemoveTap delete: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao remover tap"})
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "tap não encontrado"})
		return
	}

	_, err = tx.Exec(`
		UPDATE radar_taps SET is_mutual = false
		WHERE event_id     = $1
		  AND from_user_id = $2
		  AND to_user_id   = $3
	`, eventID, toUserID, fromUserID)
	if err != nil {
		log.Printf("RemoveTap unmutual: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao desfazer match"})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao confirmar remoção"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "tap removido"})
}

func BlockRadarUser(c *gin.Context) {
	blockerID, _ := c.Get("userID")
	blockedID := c.Param("targetUserId")

	if blockerID.(string) == blockedID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ação inválida"})
		return
	}

	db := config.GetDB()

	_, err := db.Exec(`
		INSERT INTO radar_blocks (id, blocker_user_id, blocked_user_id, created_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT DO NOTHING
	`, uuid.New().String(), blockerID, blockedID)
	if err != nil {
		log.Printf("BlockRadarUser insert: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao bloquear usuário"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"blocked": true})
}

func UnblockRadarUser(c *gin.Context) {
	blockerID, _ := c.Get("userID")
	blockedID := c.Param("targetUserId")

	db := config.GetDB()

	result, err := db.Exec(`
		DELETE FROM radar_blocks
		WHERE blocker_user_id = $1 AND blocked_user_id = $2
	`, blockerID, blockedID)
	if err != nil {
		log.Printf("UnblockRadarUser delete: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao desbloquear usuário"})
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "bloqueio não encontrado"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"blocked": false})
}

func GetBlockedUsers(c *gin.Context) {
	userID, _ := c.Get("userID")
	db := config.GetDB()

	rows, err := db.Query(`
		SELECT
			u.id,
			u.full_name,
			COALESCE(u.avatar_url, '') AS avatar_url
		FROM radar_blocks rb
		JOIN users u ON u.id = rb.blocked_user_id
		WHERE rb.blocker_user_id = $1
		ORDER BY rb.created_at DESC
	`, userID)
	if err != nil {
		log.Printf("GetBlockedUsers query: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}
	defer rows.Close()

	blockedUsers := []BlockedProfile{}
	for rows.Next() {
		var p BlockedProfile
		if err := rows.Scan(&p.UserID, &p.Name, &p.AvatarURL); err != nil {
			log.Printf("GetBlockedUsers scan: %v", err)
			continue
		}
		blockedUsers = append(blockedUsers, p)
	}

	c.JSON(http.StatusOK, gin.H{"users": blockedUsers})
}