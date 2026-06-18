// internal/user/service.go
package user

import "github.com/honor/fastkart-backend/pkg/database"

type Service struct{}

func NewService() *Service { return &Service{} }

func (s *Service) GetByID(id string) (map[string]interface{}, error) {
	var name, phone, email, avatarURL, defaultAddr string
	var walletBalance float64
	var points int

	err := database.DB.QueryRow(`
		SELECT COALESCE(name,''), phone, COALESCE(email,''),
		       COALESCE(avatar_url,''), wallet_balance, points,
		       COALESCE(default_address,'')
		FROM users WHERE id = $1`, id,
	).Scan(&name, &phone, &email, &avatarURL, &walletBalance, &points, &defaultAddr)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id": id, "name": name, "phone": phone, "email": email,
		"avatar_url": avatarURL, "wallet_balance": walletBalance,
		"points": points, "default_address": defaultAddr,
	}, nil
}

func (s *Service) Update(id, name, email string) error {
	_, err := database.DB.Exec(
		`UPDATE users SET name=$1, email=$2 WHERE id=$3`, name, email, id)
	return err
}

func (s *Service) GetAddresses(userID string) ([]map[string]interface{}, error) {
	rows, err := database.DB.Query(`
		SELECT id, label, COALESCE(name,''), COALESCE(phone,''),
		       line1, COALESCE(city,''), COALESCE(pincode,''), is_default
		FROM addresses WHERE user_id = $1 ORDER BY is_default DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var addrs []map[string]interface{}
	for rows.Next() {
		var id, label, name, phone, line1, city, pincode string
		var isDefault bool
		if err := rows.Scan(&id, &label, &name, &phone,
			&line1, &city, &pincode, &isDefault); err != nil {
			continue
		}
		addrs = append(addrs, map[string]interface{}{
			"id": id, "label": label, "name": name, "phone": phone,
			"line1": line1, "city": city, "pincode": pincode,
			"is_default": isDefault,
		})
	}
	return addrs, nil
}

func (s *Service) AddAddress(req map[string]interface{}) error {
	_, err := database.DB.Exec(`
		INSERT INTO addresses (user_id, label, name, phone, line1, city, pincode)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		req["user_id"], req["label"], req["name"], req["phone"],
		req["line1"], req["city"], req["pincode"])
	return err
}

func (s *Service) GetWallet(userID string) (map[string]interface{}, error) {
	var balance float64
	var points int
	err := database.DB.QueryRow(
		`SELECT wallet_balance, points FROM users WHERE id = $1`, userID,
	).Scan(&balance, &points)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"balance": balance, "points": points,
	}, nil
}