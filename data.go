package main

import (
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"sync"
  "time"
	"regexp"
	"strconv"
	"strings"

  //"goji.io"
//"goji.io/pat"
"gopkg.in/mgo.v2"
"gopkg.in/mgo.v2/bson"

)

