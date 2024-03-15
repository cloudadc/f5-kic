const express = require('express');
const app = express();

function handle(signal) {
    console.log(`Received ${signal}`);
    process.exit(0)
}

process.on('SIGINT', handle);
process.on('SIGTERM', handle);

const ParserModule = require('../lib/adcParser');
const parser = new ParserModule("abc")
parser.loadSchemas()

app.use(express.json({limit:'100mb'}));
app.post('/validate', function (req, res) {
    console.log(req.body);

    let valid = parser.validator(req.body)
    if (valid) {
        res.send(JSON.stringify(req.body))
    } else {
        res.status(400)
        errs = []
        parser.validator.errors.forEach(element => {
            // console.log(element)
            errs.push({
                "dataPath": element["dataPath"],
                "message": element["message"]
            })
        });
        res.send(JSON.stringify(errs))
    }
})
app.get("/*", function (req, res) {
    res.send("hello world.")
})



var server = app.listen(8081, function () {
 
  var host = server.address().address
  var port = server.address().port
 
  console.log("service started at http://%s:%s", host, port)
 
})
