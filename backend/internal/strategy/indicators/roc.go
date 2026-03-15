package indicators

func ROC(data []float64, period int) float64 {
	if len(data) < period+1 || period <= 0 {
		return 0
	}
	
	current := data[len(data)-1]
	past := data[len(data)-1-period]
	
	if past == 0 {
		return 0
	}
	
	return (current - past) / past * 100
}
