package broker

import (
	"github.com/rabbitmq/amqp091-go"
)

type RabbitMQ struct {
	Conn *amqp091.Connection
	Chan *amqp091.Channel
}

func NewRabbitMQ(url string) (*RabbitMQ, error) {
	conn, err := amqp091.Dial(url)
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}

	err = ch.ExchangeDeclare(
		"dlx.exchange",
		"direct",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return nil, err
	}

	_, err = ch.QueueDeclare(
		"reservations.expired",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return nil, err
	}

	err = ch.QueueBind(
		"reservations.expired",
		"expired.routing.key",
		"dlx.exchange",
		false,
		nil,
	)
	if err != nil {
		return nil, err
	}

	args := amqp091.Table{
		"x-dead-letter-exchange":    "dlx.exchange",
		"x-dead-letter-routing-key": "expired.routing.key",
		"x-message-ttl":             600000,
	}

	_, err = ch.QueueDeclare(
		"reservations.pending",
		true,
		false,
		false,
		false,
		args,
	)
	if err != nil {
		return nil, err
	}

	return &RabbitMQ{
		Conn: conn,
		Chan: ch,
	}, nil
}

func (r *RabbitMQ) Close() {
	r.Chan.Close()
	r.Conn.Close()
}
