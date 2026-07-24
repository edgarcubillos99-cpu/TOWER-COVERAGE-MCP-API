package rabbitmq

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RateLimiter reparte "tokens" por una cola compartida para limitar peticiones/segundo
// entre varias instancias del servicio.
type RateLimiter struct {
	url      string
	queue    string
	rps      float64
	burst    int
	leader   bool
	conn     *amqp.Connection
	mu       sync.Mutex
	closed   bool
	stopLead chan struct{}
}

type Options struct {
	URL    string
	Queue  string
	RPS    float64
	Burst  int
	Leader bool
}

// NewRateLimiter conecta a RabbitMQ, declara la cola de tokens y, si Leader=true,
// empieza a publicar COVERAGE_MAX_RPS tokens por segundo.
func NewRateLimiter(opts Options) (*RateLimiter, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("RABBITMQ_URL vacío")
	}
	if opts.Queue == "" {
		opts.Queue = "towercoverage.rate.tokens"
	}
	if opts.RPS <= 0 {
		opts.RPS = 2
	}
	if opts.Burst <= 0 {
		opts.Burst = int(opts.RPS)
		if opts.Burst < 1 {
			opts.Burst = 1
		}
	}

	rl := &RateLimiter{
		url:      opts.URL,
		queue:    opts.Queue,
		rps:      opts.RPS,
		burst:    opts.Burst,
		leader:   opts.Leader,
		stopLead: make(chan struct{}),
	}
	if err := rl.connect(); err != nil {
		return nil, err
	}
	if opts.Leader {
		go rl.runLeader()
		log.Printf("RabbitMQ rate leader activo: %.2f token(s)/s → cola %q (burst=%d)", opts.RPS, opts.Queue, opts.Burst)
	} else {
		log.Printf("RabbitMQ rate limiter conectado a cola %q (%.2f req/s compartidos; esta instancia no publica tokens)", opts.Queue, opts.RPS)
	}
	return rl, nil
}

func (rl *RateLimiter) connect() error {
	conn, err := amqp.Dial(rl.url)
	if err != nil {
		return fmt.Errorf("conectando a RabbitMQ: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("abriendo canal RabbitMQ: %w", err)
	}
	defer ch.Close()

	_, err = ch.QueueDeclare(
		rl.queue,
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		amqp.Table{
			"x-max-length": int32(rl.burst),
			"x-overflow":   "drop-head", // si se llena, descarta tokens viejos
		},
	)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("declarando cola %s: %w", rl.queue, err)
	}

	rl.mu.Lock()
	rl.conn = conn
	rl.mu.Unlock()
	return nil
}

func (rl *RateLimiter) channel() (*amqp.Channel, error) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if rl.closed {
		return nil, fmt.Errorf("rate limiter cerrado")
	}
	if rl.conn == nil || rl.conn.IsClosed() {
		if err := rl.reconnectLocked(); err != nil {
			return nil, err
		}
	}
	return rl.conn.Channel()
}

func (rl *RateLimiter) reconnectLocked() error {
	if rl.conn != nil {
		_ = rl.conn.Close()
		rl.conn = nil
	}
	conn, err := amqp.Dial(rl.url)
	if err != nil {
		return fmt.Errorf("reconectando RabbitMQ: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return err
	}
	_, err = ch.QueueDeclare(
		rl.queue,
		true, false, false, false,
		amqp.Table{
			"x-max-length": int32(rl.burst),
			"x-overflow":   "drop-head",
		},
	)
	_ = ch.Close()
	if err != nil {
		_ = conn.Close()
		return err
	}
	rl.conn = conn
	return nil
}

// Acquire espera un token de la cola compartida (bloquea hasta obtenerlo o cancelar ctx).
func (rl *RateLimiter) Acquire(ctx context.Context) error {
	if rl == nil {
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("timeout esperando cupo TowerCoverage: %w", err)
		}
		ch, err := rl.channel()
		if err != nil {
			log.Printf("⚠️ RabbitMQ rate: %v — reintento en 500ms", err)
			select {
			case <-ctx.Done():
				return fmt.Errorf("timeout esperando cupo TowerCoverage: %w", ctx.Err())
			case <-time.After(500 * time.Millisecond):
				continue
			}
		}

		msg, ok, err := ch.Get(rl.queue, true /* autoAck */)
		_ = ch.Close()
		if err != nil {
			log.Printf("⚠️ RabbitMQ Get token: %v — reintento", err)
			select {
			case <-ctx.Done():
				return fmt.Errorf("timeout esperando cupo TowerCoverage: %w", ctx.Err())
			case <-time.After(200 * time.Millisecond):
				continue
			}
		}
		if ok {
			_ = msg // token consumido
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout esperando cupo TowerCoverage: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (rl *RateLimiter) runLeader() {
	interval := time.Duration(float64(time.Second) / rl.rps)
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Rellena el burst inicial para no bloquear el arranque.
	for i := 0; i < rl.burst; i++ {
		if err := rl.publishToken(); err != nil {
			log.Printf("⚠️ RabbitMQ token inicial: %v", err)
			break
		}
	}

	for {
		select {
		case <-rl.stopLead:
			return
		case <-ticker.C:
			if err := rl.publishToken(); err != nil {
				log.Printf("⚠️ RabbitMQ publicando token: %v", err)
			}
		}
	}
}

func (rl *RateLimiter) publishToken() error {
	ch, err := rl.channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	return ch.PublishWithContext(context.Background(), "", rl.queue, false, false, amqp.Publishing{
		ContentType:  "application/octet-stream",
		Body:         []byte("1"),
		DeliveryMode: amqp.Transient,
		Timestamp:    time.Now(),
	})
}

// Close detiene el leader y cierra la conexión.
func (rl *RateLimiter) Close() {
	if rl == nil {
		return
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if rl.closed {
		return
	}
	rl.closed = true
	if rl.leader {
		close(rl.stopLead)
	}
	if rl.conn != nil {
		_ = rl.conn.Close()
		rl.conn = nil
	}
}
