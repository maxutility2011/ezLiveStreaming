package job_sqs

import (
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

func (sender SqsSender) SendMsg(data string, dedupId string) error {
	result, err := sender.GetQueueURL(&sender.QueueName)
	if err != nil {
		return err
	}

	queueURL := result.QueueUrl

	_, err1 := sender.SqsClient.SendMessage(&sqs.SendMessageInput{
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
				StringValue: aws.String("8"),
			},
		},
		MessageGroupId: aws.String("livejob"),
		MessageBody: aws.String(data),
		QueueUrl:    queueURL,
	})
	
	if err1 != nil {
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
}