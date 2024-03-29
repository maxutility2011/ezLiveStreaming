package job_sqs

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
)

type SqsSender struct {
	QueueName string
	SqsClient *sqs.SQS
}

func (sender SqsSender) GetQueueURL(queue *string) (*sqs.GetQueueUrlOutput, error) {
	result, err := sender.SqsClient.GetQueueUrl(&sqs.GetQueueUrlInput{
		QueueName: queue,
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

func (sender SqsSender) Send(data string) error {
	result, err := sender.GetQueueURL(&sender.QueueName)
	if err != nil {
		fmt.Println("Got an error getting the queue URL:")
		fmt.Println(err)
		return err
	}

	queueURL := result.QueueUrl
	//fmt.Println("Queue URL: ", *queueURL)

	_, err1 := sender.SqsClient.SendMessage(&sqs.SendMessageInput{
		//DelaySeconds: aws.Int64(10),
		MessageAttributes: map[string]*sqs.MessageAttributeValue{
			"Title": &sqs.MessageAttributeValue{
				DataType:    aws.String("String"),
				StringValue: aws.String("TestMessage"),
			},
			"Author": &sqs.MessageAttributeValue{
				DataType:    aws.String("String"),
				StringValue: aws.String("Bo Zhang"),
			},
			"WeeksOn": &sqs.MessageAttributeValue{
				DataType:    aws.String("Number"),
				StringValue: aws.String("6"),
			},
		},
		MessageGroupId: aws.String("livejob"),
		MessageBody: aws.String(data),
		QueueUrl:    queueURL,
	})
	
	if err1 != nil {
		fmt.Println("Failed to send message: ", err1)
		return err1
	}

	return nil
}

func (sender SqsSender) CreateClient() *sqs.SQS {
	sess, _ := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1")},
	)

	client := sqs.New(sess)
	return client
	
	/*
	sess, _ := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1")},
	)

	svc := sqs.New(sess)
	qs, _ := svc.ListQueues(nil)

	for i, url := range qs.QueueUrls {
		fmt.Printf("%d: %s\n", i, *url)
	}
	*/
}