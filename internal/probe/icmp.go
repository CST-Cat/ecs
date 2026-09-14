package probe

import "math"

// icmpStats 是一次 ICMP 探测的统计结果。
type icmpStats struct {
	Available   bool
	LossKnown   bool
	LossPercent float64
	RTTKnown    bool
	MinMS       float64
	AvgMS       float64
	MaxMS       float64
	StdDevMS    float64
	// StdDevKnown 区分"标准差为 0"和"这个 ping 实现没有报告标准差"。
	StdDevKnown bool
	Err         error
}

// icmpSample is the platform-neutral result of one native or Unix echo
// attempt. Sent is true only after the platform adapter has issued the
// request; Received and RTTMS are populated from a validated reply.
type icmpSample struct {
	Sent     bool
	Received bool
	RTTMS    float64
}

// aggregateICMPSamples converts native per-request facts to the stable ICMP
// result consumed by latency. It deliberately treats a completed request with
// no reply as known loss, while RTT and standard deviation remain unknown until
// at least one valid reply exists.
func aggregateICMPSamples(samples []icmpSample) icmpStats {
	stats := icmpStats{}
	var rtts []float64
	sent := 0
	received := 0
	for _, sample := range samples {
		if !sample.Sent {
			continue
		}
		sent++
		stats.Available = true
		if sample.Received {
			received++
			if sample.RTTMS >= 0 && !math.IsNaN(sample.RTTMS) && !math.IsInf(sample.RTTMS, 0) {
				rtts = append(rtts, sample.RTTMS)
			}
		}
	}
	if sent == 0 || !stats.Available {
		return stats
	}
	stats.LossKnown = true
	stats.LossPercent = float64(sent-received) / float64(sent) * 100
	if len(rtts) == 0 {
		return stats
	}
	stats.RTTKnown = true
	stats.MinMS, stats.MaxMS = rtts[0], rtts[0]
	var total float64
	for _, value := range rtts {
		if value < stats.MinMS {
			stats.MinMS = value
		}
		if value > stats.MaxMS {
			stats.MaxMS = value
		}
		total += value
	}
	stats.AvgMS = total / float64(len(rtts))
	var squared float64
	for _, value := range rtts {
		delta := value - stats.AvgMS
		squared += delta * delta
	}
	stats.StdDevMS = math.Sqrt(squared / float64(len(rtts)))
	stats.StdDevKnown = true
	return stats
}
