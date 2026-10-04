package client

import (
	"log"

	"bilheteria-api/config"
	"bilheteria-api/services/orderservice"
)

// notifyTicketTransfer dispara o e-mail de "você recebeu um ingresso" para o
// destinatário, em background.
//
// Roda como goroutine e engole o erro de propósito: a transferência já foi
// commitada quando esta função é chamada. O destinatário sempre encontra o
// ingresso em /meus-ingressos mesmo sem o e-mail, então o pior caso é a
// notificação não ter saído — o que precisa aparecer é no log, não como 500
// para quem acabou de perder o ingresso.
func notifyTicketTransfer(res transferResult) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("notifyTicketTransfer: panic: %v", r)
		}
	}()

	err := orderservice.SendTransferNotification(config.GetDB(), defaultEmailSender, orderservice.TransferNotificationInput{
		RecipientID:    res.RecipientID,
		RecipientName:  res.RecipientName,
		RecipientEmail: res.RecipientEmail,
		RecipientGuest: res.RecipientGuest,
		SenderCPF:      res.SenderCPF,
		EventID:        res.EventID,
		EventName:      res.EventName,
		TicketID:       res.TicketID,
		LoteName:       res.LoteName,
		NewQRCode:      res.NewQRCode,
	})
	if err != nil {
		log.Printf("notifyTicketTransfer: %v", err)
		return
	}
	log.Printf("notifyTicketTransfer: destinatário %s notificado ✓", res.RecipientID)
}