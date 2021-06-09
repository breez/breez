package main

import (
	"fmt"
	"log"

	"github.com/breez/breez/backup"
)

type ServicesImpl struct {
}

func (a *ServicesImpl) Notify(notificationEvent []byte) {}

func (a *ServicesImpl) BackupProviderSignIn() (string, error) {
	return "", nil
}

func (a *ServicesImpl) BackupProviderName() string {
	return "gdrive"
}

type AuthService struct {
}

// SignIn is the interface function implementation needed for backup.Manager
func (a *AuthService) SignIn() (string, error) {
	return `
	{
		"user": "roeierez@gmail.com",
		"password": "jxPpx-zpdXe-H5CnK-xZsLA-TYPfS",
		"root": "https://nextcloud05.webo.cloud/remote.php/dav/files/roeierez@gmail.com",
		"breezDir":"breez"
	}
	`, nil
}

func main() {

	p, err := backup.NewNextCloudProvider(backup.ProviderData{
		User:     "itzik.proj@gmail.com",
		Password: "TsLNd-EbctW-ReBnZ-BGpFM-AZKM3",
		Url:      "https://nextcloud05.webo.cloud",
		BreezDir: "breez3",
	}, nil)
	// p, err := backup.NewNextCloudProvider(backup.ProviderData{
	// 	User:     "XCFMsQsm9wprE5bo",
	// 	Password: "demo",
	// 	Url:      "https://demo2.nextcloud.com/",
	// 	BreezDir: "breez2",
	// }, nil)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	//snapshots, err := p.AvailableSnapshots()
	_, err = p.UploadBackupFiles("/Users/roeierez/lnd.conf", "123456", "")
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	snapshots, err := p.AvailableSnapshots()
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	for _, s := range snapshots {
		fmt.Printf("snapshot: %v\n", s.BackupID)
		files, err := p.DownloadBackupFiles(s.NodeID, "123")
		if err != nil {
			return
		}
		fmt.Println(files)
	}

	// files, err := p.DownloadBackupFiles(snapshots[0].NodeID, snapshots[0].BackupID)
	// if err != nil {
	// 	log.Fatalf("error: %v", err)
	// }

	// fmt.Println(strings.Join(files, ","))

	// for _, s := range snapshots {
	// 	fmt.Println(s.NodeID)
	// }

	// workingDir := os.Getenv("LND_DIR")
	// err := bindings.Init(os.TempDir(), workingDir, &ServicesImpl{})
	// if err != nil {
	// 	fmt.Println("Error in binding.Init", err)
	// 	os.Exit(1)
	// }

	// err = bindings.Start()
	// if err != nil {
	// 	fmt.Println("Error in binding.Start", err)
	// 	os.Exit(1)
	// }

	// reader := bufio.NewReader(os.Stdin)
	// _, _ = reader.ReadString('\n')

}
